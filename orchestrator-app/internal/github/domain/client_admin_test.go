package domain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_ListWebhooks_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/hooks" {
			t.Errorf("path = %s, want /repos/acme/widgets/hooks", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ghp_admin" {
			t.Errorf("auth = %s, want Bearer ghp_admin", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id": 42, "active": true, "events": ["pull_request", "issues"], "config": {"url": "https://api.example.com/webhook", "content_type": "json", "insecure_ssl": "0"}},
			{"id": 99, "active": false, "events": ["push"], "config": {"url": "https://other.example.com/hook", "content_type": "form", "insecure_ssl": "0"}}
		]`))
	}))
	defer server.Close()

	c := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	hooks, err := c.ListWebhooks(context.Background(), "acme", "widgets", "ghp_admin")
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("len(hooks) = %d, want 2", len(hooks))
	}
	if hooks[0].ID != 42 || hooks[0].URL != "https://api.example.com/webhook" || !hooks[0].Active {
		t.Errorf("hooks[0] = %+v, want id=42 url=https://api.example.com/webhook active=true", hooks[0])
	}
	if len(hooks[0].Events) != 2 || hooks[0].Events[0] != "pull_request" {
		t.Errorf("hooks[0].Events = %v, want [pull_request, issues]", hooks[0].Events)
	}
}

func TestClient_ListWebhooks_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	c := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	_, err := c.ListWebhooks(context.Background(), "acme", "widgets", "ghp_admin")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestClient_CreateWebhook_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/hooks" {
			t.Errorf("path = %s, want /repos/acme/widgets/hooks", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["name"] != "web" {
			t.Errorf("body.name = %v, want web", got["name"])
		}
		evs, _ := got["events"].([]any)
		if len(evs) != 3 {
			t.Errorf("body.events = %v, want 3 entries", evs)
		}
		cfg, _ := got["config"].(map[string]any)
		if cfg["url"] != "https://api.example.com/webhook" {
			t.Errorf("body.config.url = %v", cfg["url"])
		}
		if cfg["secret"] != "whsec123" {
			t.Errorf("body.config.secret = %v, want whsec123", cfg["secret"])
		}
		if cfg["content_type"] != "json" {
			t.Errorf("body.config.content_type = %v, want json", cfg["content_type"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 12345}`))
	}))
	defer server.Close()

	c := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	desired := Webhook{
		URL:    "https://api.example.com/webhook",
		Secret: "whsec123",
		Events: []string{"pull_request", "issues", "issue_comment"},
		Active: true,
	}
	if err := c.CreateWebhook(context.Background(), "acme", "widgets", "ghp_admin", desired); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
}

func TestClient_UpdateWebhook_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/hooks/12345" {
			t.Errorf("path = %s, want /repos/acme/widgets/hooks/12345", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		// URL/secret are part of config block; events/active are top-level
		cfg, _ := got["config"].(map[string]any)
		if cfg["secret"] != "rotated_secret" {
			t.Errorf("config.secret = %v", cfg["secret"])
		}
		if got["active"] != true {
			t.Errorf("active = %v, want true", got["active"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": 12345}`))
	}))
	defer server.Close()

	c := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	desired := Webhook{
		URL:    "https://api.example.com/webhook",
		Secret: "rotated_secret",
		Events: []string{"pull_request", "issues", "issue_comment"},
		Active: true,
	}
	if err := c.UpdateWebhook(context.Background(), "acme", "widgets", 12345, "ghp_admin", desired); err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
}

func TestClient_GetActionsSecretPublicKey_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widgets/actions/secrets/public-key" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key_id": "kid-001", "key": "BASE64ENCODEDPUBKEY=="}`))
	}))
	defer server.Close()

	c := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	keyID, pubKey, err := c.GetActionsSecretPublicKey(context.Background(), "acme", "widgets", "ghp_admin")
	if err != nil {
		t.Fatalf("GetActionsSecretPublicKey: %v", err)
	}
	if keyID != "kid-001" {
		t.Errorf("keyID = %s, want kid-001", keyID)
	}
	if pubKey != "BASE64ENCODEDPUBKEY==" {
		t.Errorf("pubKey = %s", pubKey)
	}
}

func TestClient_PutActionsSecret_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/actions/secrets/FIREWORKS_API_KEY" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		if !strings.Contains(s, `"encrypted_value":"ENCRYPTED_BLOB"`) {
			t.Errorf("body missing encrypted_value: %s", s)
		}
		if !strings.Contains(s, `"key_id":"kid-001"`) {
			t.Errorf("body missing key_id: %s", s)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	c := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	if err := c.PutActionsSecret(context.Background(), "acme", "widgets", "FIREWORKS_API_KEY", "ENCRYPTED_BLOB", "kid-001", "ghp_admin"); err != nil {
		t.Fatalf("PutActionsSecret: %v", err)
	}
}
