package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

const testSigningSecret = "test-signing-secret-123"

// --- Mock implementations ---

type mockAgentRunner struct {
	mu       sync.Mutex
	response agent.RunResponse
	err      error
	called   bool
	lastReq  agent.RunRequest
}

func (m *mockAgentRunner) Run(_ context.Context, req agent.RunRequest) (agent.RunResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.lastReq = req
	return m.response, m.err
}

func (m *mockAgentRunner) wasCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

func (m *mockAgentRunner) getLastReq() agent.RunRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastReq
}

type mockThreadFinder struct {
	mu        sync.Mutex
	thread    *domain.SlackThread
	err       error
	callCount int
}

func (m *mockThreadFinder) FindThreadBySlackTs(_ context.Context, _ string) (*domain.SlackThread, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.thread, nil
}

func (m *mockThreadFinder) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

type mockThreadSaver struct {
	mu      sync.Mutex
	saved   []*domain.SlackThread
	saveErr error
}

func (m *mockThreadSaver) SaveThread(_ context.Context, thread *domain.SlackThread) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = append(m.saved, thread)
	return nil
}

func (m *mockThreadSaver) getSaved() []*domain.SlackThread {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved
}

type mockConversationRecorder struct {
	mu    sync.Mutex
	convs []agent.Conversation
	err   error
}

func (m *mockConversationRecorder) UpsertConversation(_ context.Context, conv agent.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.convs = append(m.convs, conv)
	return nil
}

func (m *mockConversationRecorder) getConversations() []agent.Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.convs
}

type mockSlackClient struct {
	mu              sync.Mutex
	channelName     string
	channelErr      error
	userName        string
	userErr         error
	postedMessages  []string
	reactions       []string
	removedEmoji    []string
	postToThreadErr error
}

func (m *mockSlackClient) GetUserInfo(_ context.Context, _, _ string) (string, error) {
	return m.userName, m.userErr
}

func (m *mockSlackClient) GetChannelName(_ context.Context, _, _ string) (string, error) {
	return m.channelName, m.channelErr
}

func (m *mockSlackClient) PostMessage(_ context.Context, _, _ string, msg domain.MessageBlock) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postedMessages = append(m.postedMessages, msg.Text)
	return "1234.5678", nil
}

func (m *mockSlackClient) PostToThread(_ context.Context, _, _, _ string, msg domain.MessageBlock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postedMessages = append(m.postedMessages, msg.Text)
	return m.postToThreadErr
}

func (m *mockSlackClient) AddReaction(_ context.Context, _, _, _, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions = append(m.reactions, emoji)
	return nil
}

func (m *mockSlackClient) RemoveReaction(_ context.Context, _, _, _, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedEmoji = append(m.removedEmoji, emoji)
	return nil
}

func (m *mockSlackClient) getReactions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reactions
}

func (m *mockSlackClient) getRemovedEmoji() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removedEmoji
}

func (m *mockSlackClient) getPostedMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.postedMessages
}

type mockTargetRegistry struct {
	config        targets.TargetConfig
	found         bool
	channelConfig targets.TargetConfig
	channelFound  bool
	botToken      string
}

func (m *mockTargetRegistry) Get(_, _ string) (targets.TargetConfig, bool) {
	return m.config, m.found
}

func (m *mockTargetRegistry) GetByChannelName(_ string) (targets.TargetConfig, bool) {
	return m.channelConfig, m.channelFound
}

func (m *mockTargetRegistry) AnyBotToken() string {
	return m.botToken
}

// callbackInvokingRunner is an AgentRunner that invokes OnIssueCreated during Run()
type callbackInvokingRunner struct {
	mu                sync.Mutex
	response          agent.RunResponse
	called            bool
	lastReq           agent.RunRequest
	savedBeforeReturn bool
	threadSaver       *mockThreadSaver
}

func (r *callbackInvokingRunner) Run(_ context.Context, req agent.RunRequest) (agent.RunResponse, error) {
	r.mu.Lock()
	r.called = true
	r.lastReq = req
	r.mu.Unlock()

	// Simulate tool execution invoking the callback during Run
	if req.OnIssueCreated != nil {
		req.OnIssueCreated("example-org", "playground", 42, "New bug")
	}

	// Check if the thread was saved during the callback
	saved := r.threadSaver.getSaved()
	r.mu.Lock()
	r.savedBeforeReturn = len(saved) > 0
	r.mu.Unlock()

	return r.response, nil
}

func (r *callbackInvokingRunner) wasCalled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.called
}

type mockFeatureAssembler struct{}

func (m *mockFeatureAssembler) ForPR(_ context.Context, _, _, _ string, _, _ int) (*featurecontext.FeatureSnapshot, error) {
	return nil, nil
}

func (m *mockFeatureAssembler) ForIssue(_ context.Context, _, _, _ string, _ int) (*featurecontext.FeatureSnapshot, error) {
	return nil, nil
}

// --- Helpers ---

func signRequest(body []byte, timestamp string) string {
	sigBase := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func makeSignedRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Signature", signRequest(body, timestamp))
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	return req
}

func defaultTarget() targets.TargetConfig {
	return targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "playground",
		GitHubPAT:     "ghp_test123",
		SlackBotToken: "xoxb-test",
	}
}

// --- Tests ---

func TestHandleEvent_URLVerification(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, testSigningSecret, "")

	payload := map[string]string{
		"type":      "url_verification",
		"challenge": "abc123challenge",
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp["challenge"] != "abc123challenge" {
		t.Errorf("Expected challenge abc123challenge, got %s", resp["challenge"])
	}
}

func TestHandleEvent_BadSignature(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, testSigningSecret, "")

	body := []byte(`{"type":"url_verification","challenge":"test"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Signature", "v0=invalid-signature")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)

	rec := httptest.NewRecorder()
	h.HandleEvent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}
}

func TestHandleEvent_StaleTimestamp(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, testSigningSecret, "")

	body := []byte(`{"type":"url_verification","challenge":"test"}`)
	staleTs := strconv.FormatInt(time.Now().Unix()-600, 10)

	sigBase := "v0:" + staleTs + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Signature", sig)
	req.Header.Set("X-Slack-Request-Timestamp", staleTs)

	rec := httptest.NewRecorder()
	h.HandleEvent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for stale timestamp, got %d", rec.Code)
	}
}

func TestHandleEvent_MissingSignatureHeaders(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, testSigningSecret, "")

	body := []byte(`{"type":"url_verification","challenge":"test"}`)
	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))

	rec := httptest.NewRecorder()
	h.HandleEvent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for missing headers, got %d", rec.Code)
	}
}

func TestHandleEvent_TopLevelMention_AgentRunsAndResponds(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "I created issue #42 for you!",
			SideEffects: agent.SideEffects{
				CreatedIssues: []agent.CreatedIssue{{Number: 42, Title: "Dark mode"}},
			},
		},
	}
	threadSaver := &mockThreadSaver{}
	slackClient := &mockSlackClient{
		userName:    "Alice Smith",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	convRecorder := &mockConversationRecorder{}
	h := NewHandler(agentRunner, &mockThreadFinder{}, threadSaver, convRecorder, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> Add dark mode support",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(200 * time.Millisecond)

	if !agentRunner.wasCalled() {
		t.Fatal("Expected agent to be called")
	}

	lastReq := agentRunner.getLastReq()
	if lastReq.UserText != "Add dark mode support" {
		t.Errorf("Expected stripped text, got %q", lastReq.UserText)
	}
	if lastReq.UserName != "Alice Smith" {
		t.Errorf("Expected user name 'Alice Smith', got %q", lastReq.UserName)
	}

	// Check reactions: eyes added, then removed, then white_check_mark added
	reactions := slackClient.getReactions()
	if len(reactions) < 2 || reactions[0] != "eyes" || reactions[1] != "white_check_mark" {
		t.Errorf("Expected [eyes, white_check_mark] reactions, got %v", reactions)
	}

	removed := slackClient.getRemovedEmoji()
	if len(removed) < 1 || removed[0] != "eyes" {
		t.Errorf("Expected eyes removed, got %v", removed)
	}

	// Check thread mapping saved
	saved := threadSaver.getSaved()
	if len(saved) != 1 {
		t.Fatalf("Expected 1 saved thread, got %d", len(saved))
	}
	if saved[0].GithubIssueID != 42 {
		t.Errorf("Expected issue ID 42, got %d", saved[0].GithubIssueID)
	}

	// Check response posted
	msgs := slackClient.getPostedMessages()
	if len(msgs) != 1 || msgs[0] != "I created issue #42 for you!" {
		t.Errorf("Expected agent response posted, got %v", msgs)
	}

	// Check conversation recorded
	convs := convRecorder.getConversations()
	if len(convs) != 1 {
		t.Fatalf("Expected 1 recorded conversation, got %d", len(convs))
	}
	if convs[0].ChannelID != "C0PRODUCT" {
		t.Errorf("Expected channel C0PRODUCT, got %s", convs[0].ChannelID)
	}
	if convs[0].LinkedIssue != 42 {
		t.Errorf("Expected linked issue 42, got %d", convs[0].LinkedIssue)
	}
	if convs[0].UserName != "Alice Smith" {
		t.Errorf("Expected user name 'Alice Smith', got %q", convs[0].UserName)
	}
}

func TestHandleEvent_DelegatedIssues_ThreadMappingSaved(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "I've delegated to OpenCode.",
			SideEffects: agent.SideEffects{
				DelegatedIssues: []int{10, 20},
			},
		},
	}
	threadSaver := &mockThreadSaver{}
	slackClient := &mockSlackClient{
		userName:    "Alice Smith",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, threadSaver, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> delegate to opencode",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	saved := threadSaver.getSaved()
	if len(saved) != 2 {
		t.Fatalf("Expected 2 saved threads for delegated issues, got %d", len(saved))
	}
	if saved[0].GithubIssueID != 10 {
		t.Errorf("Expected first delegated issue 10, got %d", saved[0].GithubIssueID)
	}
	if saved[1].GithubIssueID != 20 {
		t.Errorf("Expected second delegated issue 20, got %d", saved[1].GithubIssueID)
	}
}

func TestHandleEvent_DelegatedIssues_SkipsAlreadyMappedFromCreatedIssues(t *testing.T) {
	// If an issue is both created AND delegated, only one mapping should be saved
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "Created and delegated.",
			SideEffects: agent.SideEffects{
				CreatedIssues:   []agent.CreatedIssue{{Number: 10, Title: "New issue"}},
				DelegatedIssues: []int{10}, // same issue
			},
		},
	}
	threadSaver := &mockThreadSaver{}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, threadSaver, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> create and delegate",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	saved := threadSaver.getSaved()
	// Should have exactly 1 mapping (from CreatedIssues), not 2
	if len(saved) != 1 {
		t.Fatalf("Expected 1 saved thread (dedup), got %d", len(saved))
	}
	if saved[0].GithubIssueID != 10 {
		t.Errorf("Expected issue 10, got %d", saved[0].GithubIssueID)
	}
}

func TestHandleEvent_InThreadMention_AgentRunsWithContext(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "Done!"},
	}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			GithubIssueID: 42,
			RepoOwner:     "example-org",
			RepoName:      "playground",
		},
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, threadFinder, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> please implement this",
			"thread_ts": "1111111111.111111",
			"channel":   "C0PRODUCT",
			"ts":        "2222222222.222222",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	if !agentRunner.wasCalled() {
		t.Fatal("Expected agent to be called")
	}

	lastReq := agentRunner.getLastReq()
	if lastReq.ThreadTs != "1111111111.111111" {
		t.Errorf("Expected thread_ts passed, got %q", lastReq.ThreadTs)
	}
	if lastReq.LinkedIssue == nil || lastReq.LinkedIssue.Number != 42 {
		t.Errorf("Expected linked issue #42, got %+v", lastReq.LinkedIssue)
	}
}

func TestHandleEvent_AgentError_PostsGenericErrorMessage(t *testing.T) {
	agentRunner := &mockAgentRunner{err: fmt.Errorf("something unexpected")}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	reactions := slackClient.getReactions()
	hasX := false
	for _, r := range reactions {
		if r == "x" {
			hasX = true
		}
	}
	if !hasX {
		t.Errorf("Expected :x: reaction on error, got %v", reactions)
	}

	msgs := slackClient.getPostedMessages()
	if len(msgs) == 0 {
		t.Fatal("Expected error message posted")
	}
	if !strings.Contains(msgs[0], "encountered an error") {
		t.Errorf("Expected generic error message, got %q", msgs[0])
	}
}

func TestHandleEvent_AgentError503_PostsServiceUnavailableMessage(t *testing.T) {
	agentRunner := &mockAgentRunner{
		err: fmt.Errorf("orchestrator routing: router llm call: api error (status 503): upstream connect error"),
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	msgs := slackClient.getPostedMessages()
	if len(msgs) == 0 {
		t.Fatal("Expected error message posted")
	}
	if !strings.Contains(msgs[0], "temporarily unavailable") {
		t.Errorf("Expected service unavailable message, got %q", msgs[0])
	}
}

func TestHandleEvent_AgentError429Overloaded_PostsOverloadedMessage(t *testing.T) {
	agentRunner := &mockAgentRunner{
		err: fmt.Errorf("orchestrator routing: router llm call: api error (status 429): {\"error\":{\"object\":\"error\",\"type\":\"invalid_request_error\",\"code\":\"too_many_requests\",\"message\":\"Request didn't generate first token before the given deadline, the service is overloaded\"}}"),
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	msgs := slackClient.getPostedMessages()
	if len(msgs) == 0 {
		t.Fatal("Expected error message posted")
	}
	if !strings.Contains(msgs[0], "overloaded") {
		t.Errorf("Expected overloaded message, got %q", msgs[0])
	}
}

func TestHandleEvent_AgentError429_PostsRateLimitMessage(t *testing.T) {
	agentRunner := &mockAgentRunner{
		err: fmt.Errorf("specialist researcher: specialist researcher llm completion: api error (status 429): rate limit exceeded"),
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	msgs := slackClient.getPostedMessages()
	if len(msgs) == 0 {
		t.Fatal("Expected error message posted")
	}
	if !strings.Contains(msgs[0], "rate-limited") {
		t.Errorf("Expected rate limit message, got %q", msgs[0])
	}
}

func TestHandleEvent_AgentTimeout_PostsErrorMessage(t *testing.T) {
	// Agent blocks longer than the configured timeout
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "too late"},
	}
	// Override Run to simulate a slow agent
	slowAgent := &slowMockAgentRunner{delay: 500 * time.Millisecond}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(slowAgent, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")
	h.agentTimeout = 50 * time.Millisecond // very short timeout

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> do something slow",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(300 * time.Millisecond)

	// Should add :x: reaction due to timeout error
	reactions := slackClient.getReactions()
	hasX := false
	for _, r := range reactions {
		if r == "x" {
			hasX = true
			break
		}
	}
	if !hasX {
		t.Errorf("Expected :x: reaction on timeout, got %v", reactions)
	}

	_ = agentRunner // suppress unused warning
}

// slowMockAgentRunner simulates a slow agent that respects context cancellation.
type slowMockAgentRunner struct {
	mu      sync.Mutex
	delay   time.Duration
	called  bool
	lastReq agent.RunRequest
}

func (m *slowMockAgentRunner) Run(ctx context.Context, req agent.RunRequest) (agent.RunResponse, error) {
	m.mu.Lock()
	m.called = true
	m.lastReq = req
	m.mu.Unlock()

	select {
	case <-time.After(m.delay):
		return agent.RunResponse{Text: "done"}, nil
	case <-ctx.Done():
		return agent.RunResponse{}, ctx.Err()
	}
}

func TestHandleEvent_EmptyTextWithCreatedIssues_SynthesizesFallback(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "", // LLM returned no text
			SideEffects: agent.SideEffects{
				CreatedIssues: []agent.CreatedIssue{{Number: 49, Title: "Forgot Password"}},
			},
		},
	}
	threadSaver := &mockThreadSaver{}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, threadSaver, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> let's start fresh",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	msgs := slackClient.getPostedMessages()
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 posted message, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "#49") {
		t.Errorf("Expected fallback message containing #49, got %q", msgs[0])
	}
	if !strings.Contains(msgs[0], "Forgot Password") {
		t.Errorf("Expected fallback message containing title, got %q", msgs[0])
	}
}

func TestHandleEvent_EmptyTextWithDelegatedIssues_SynthesizesFallback(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "", // LLM returned no text
			SideEffects: agent.SideEffects{
				DelegatedIssues: []int{10, 20},
			},
		},
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> delegate these",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	msgs := slackClient.getPostedMessages()
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 posted message, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "#10") || !strings.Contains(msgs[0], "#20") {
		t.Errorf("Expected fallback message containing #10 and #20, got %q", msgs[0])
	}
}

func TestHandleEvent_PostToThreadError_LogsAndContinues(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "Here's the result!"},
	}
	slackClient := &mockSlackClient{
		userName:        "Alice",
		channelName:     "productbuilding-playground",
		postToThreadErr: fmt.Errorf("slack API timeout"),
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	// Should still add :white_check_mark: even if PostToThread fails
	reactions := slackClient.getReactions()
	hasCheckmark := false
	for _, r := range reactions {
		if r == "white_check_mark" {
			hasCheckmark = true
			break
		}
	}
	if !hasCheckmark {
		t.Errorf("Expected white_check_mark reaction despite PostToThread error, got %v", reactions)
	}
}

type mockTraceSaver struct {
	mu     sync.Mutex
	traces []TraceSaveRequest
}

func (m *mockTraceSaver) SaveTrace(_ context.Context, record TraceSaveRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.traces = append(m.traces, record)
	return nil
}

func (m *mockTraceSaver) getSaved() []TraceSaveRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.traces
}

func TestHandleEvent_SavesTraceAfterAgentRun(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "Done!"},
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}
	traceSaver := &mockTraceSaver{}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")
	h.traceSaver = traceSaver

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> check CI",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	saved := traceSaver.getSaved()
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved trace, got %d", len(saved))
	}
	if saved[0].UserName != "Alice" {
		t.Errorf("expected user 'Alice', got %q", saved[0].UserName)
	}
	if saved[0].UserText != "check CI" {
		t.Errorf("expected user text 'check CI', got %q", saved[0].UserText)
	}
	if saved[0].RepoOwner != "example-org" {
		t.Errorf("expected repo owner 'example-org', got %q", saved[0].RepoOwner)
	}
}

func TestHandleEvent_UnregisteredChannel_Ignored(t *testing.T) {
	agentRunner := &mockAgentRunner{}
	slackClient := &mockSlackClient{channelName: "random-channel"}
	registry := &mockTargetRegistry{channelFound: false, botToken: "xoxb-test"}

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C0RANDOM",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(100 * time.Millisecond)

	if agentRunner.wasCalled() {
		t.Error("Agent should not be called for unregistered channel")
	}
}

func TestHandler_AppMention_ThreadLookedUpOnce(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "Done"},
	}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			GithubIssueID: 42,
			GithubPRID:    52,
			RepoOwner:     "example-org",
			RepoName:      "playground",
		},
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, threadFinder, &mockThreadSaver{}, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")
	// Also set feature assembler to exercise the second lookup path
	h.SetFeatureAssembler(&mockFeatureAssembler{})

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> what's the status?",
			"thread_ts": "parent-ts-123",
			"channel":   "C0PRODUCT",
			"ts":        "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	count := threadFinder.getCallCount()
	if count != 1 {
		t.Errorf("Expected FindThreadBySlackTs called exactly 1 time, got %d", count)
	}
}

func TestHandler_AppMention_IssueCreated_SavesThreadImmediately(t *testing.T) {
	threadSaver := &mockThreadSaver{}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	// Agent runner that synchronously invokes OnIssueCreated within Run()
	agentRunner := &mockAgentRunner{}
	agentRunner.response = agent.RunResponse{
		Text: "Created issue #42",
		SideEffects: agent.SideEffects{
			CreatedIssues: []agent.CreatedIssue{{Number: 42, Title: "New bug"}},
		},
	}

	runner := &callbackInvokingRunner{
		response:    agentRunner.response,
		threadSaver: threadSaver,
	}

	h := NewHandler(runner, &mockThreadFinder{}, threadSaver, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> create a bug report",
			"channel": "C0PRODUCT",
			"ts":      "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	runner.mu.Lock()
	savedBefore := runner.savedBeforeReturn
	runner.mu.Unlock()

	if !savedBefore {
		t.Error("Expected thread to be saved BEFORE Run() returns (via OnIssueCreated callback)")
	}
}

func TestHandler_AppMention_NoThread_StillWorks(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "I can help!"},
	}
	threadFinder := &mockThreadFinder{thread: nil}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}

	h := NewHandler(agentRunner, threadFinder, &mockThreadSaver{}, &mockConversationRecorder{}, slackClient, registry, testSigningSecret, "")
	h.SetFeatureAssembler(&mockFeatureAssembler{})

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> hello",
			"thread_ts": "parent-ts-123",
			"channel":   "C0PRODUCT",
			"ts":        "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	if !agentRunner.wasCalled() {
		t.Fatal("Expected agent to be called")
	}

	lastReq := agentRunner.getLastReq()
	if lastReq.LinkedIssue != nil {
		t.Errorf("Expected nil LinkedIssue when no thread, got %+v", lastReq.LinkedIssue)
	}
	if lastReq.FeatureSummary != "" {
		t.Errorf("Expected empty FeatureSummary when no thread, got %q", lastReq.FeatureSummary)
	}
}
