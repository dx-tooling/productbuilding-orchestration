# SPOC Slack Bot: Implementation Plan

## Context

The Slack Bot currently has two separate brains: a **Notifier** that mechanically formats GitHub events into Slack messages (zero conversation awareness), and an **Agent** that responds to Slack mentions with LLM reasoning (limited GitHub state awareness). The goal is to unify these behind a shared **Feature Context Assembly** layer so every Slack message -- whether a proactive notification or an agent response -- reflects a complete understanding of "what's happening with this feature."

No backwards compatibility, migration logic, or legacy support needed. Strict red-green TDD throughout.

## Dependency Graph

```
Phase 1 (GitHub client) ──┐
                           ├──► Phase 2 (Feature Context Assembly)
                           │         │
                           │    ┌────┴─────┐
                           │    ▼          ▼
                           │  Phase 3    Phase 5
                           │  (Messages) (Agent context)
                           │    │
                           │    ▼
                           │  Phase 4 (Notification overhaul)
                           │    │
                           └────┤
                                ▼
                           Phase 6 (CI webhooks)

                           Phase 7 (PR comment reduction) ← independent
```

---

## Phase 1: GitHub Client Extensions

Add `GetPR` and `GetCheckRunsForRef` to the GitHub client.

### Files

| Action | File |
|--------|------|
| Modify | `internal/github/domain/client.go` |
| Modify | `internal/github/domain/client_test.go` |

### New types in `client.go`

```go
type PRDetail struct {
    Number    int
    Title     string
    Body      string
    State     string // "open", "closed"
    Merged    bool
    HeadSHA   string
    HeadRef   string
    BaseRef   string
    URL       string
    User      string
    Additions int
    Deletions int
}

type CheckRun struct {
    ID         int64
    Name       string
    Status     string // "queued", "in_progress", "completed"
    Conclusion string // "success", "failure", "neutral", etc.
    HTMLURL    string
}
```

### New methods

```go
func (c *Client) GetPR(ctx context.Context, owner, repo string, number int, pat string) (*PRDetail, error)
// GET /repos/{owner}/{repo}/pulls/{number}

func (c *Client) GetCheckRunsForRef(ctx context.Context, owner, repo, ref, pat string) ([]CheckRun, error)
// GET /repos/{owner}/{repo}/commits/{ref}/check-runs
```

### TDD cycles

1. **`TestClient_GetPR_Success`** -- httptest returns PR JSON with number=10, merged=false, head.sha="abc". Assert all PRDetail fields map correctly, correct HTTP method/path/auth header.
2. **`TestClient_GetPR_NotFound`** -- httptest returns 404. Assert error returned.
3. **`TestClient_GetCheckRunsForRef_Success`** -- httptest returns `{"check_runs":[...]}` with 2 check runs. Assert slice length and field mapping. Path: `/repos/{owner}/{repo}/commits/{ref}/check-runs`.
4. **`TestClient_GetCheckRunsForRef_Empty`** -- httptest returns empty array. Assert empty slice, no error.

---

## Phase 2: Feature Context Assembly

New cross-cutting package providing the shared context layer.

### Files

| Action | File |
|--------|------|
| Create | `internal/featurecontext/assembler.go` |
| Create | `internal/featurecontext/assembler_test.go` |
| Create | `internal/featurecontext/adapters.go` |

### Types in `assembler.go`

```go
package featurecontext

type FeatureSnapshot struct {
    Issue     *IssueState
    PR        *PRState
    CIStatus  CIStatus         // "unknown", "pending", "passing", "failing"
    CIDetails []CheckRunState
    Preview   *PreviewState
}

type IssueState struct {
    Number int
    Title  string
    Body   string
    State  string
}

type PRState struct {
    Number    int
    Title     string
    State     string
    Merged    bool
    HeadSHA   string
    HeadRef   string
    Author    string
    Additions int
    Deletions int
    URL       string
}

type CIStatus string
const (
    CIUnknown CIStatus = "unknown"
    CIPending CIStatus = "pending"
    CIPassing CIStatus = "passing"
    CIFailing CIStatus = "failing"
)

type CheckRunState struct {
    Name       string
    Conclusion string
    URL        string
}

type PreviewState struct {
    Status string // "ready", "building", "failed", etc.
    URL    string
}
```

### Source interfaces (consumer-side, in `assembler.go`)

```go
type IssueGetter interface {
    GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueState, error)
}

type PRGetter interface {
    GetPR(ctx context.Context, owner, repo string, number int, pat string) (*PRState, error)
}

type CheckRunGetter interface {
    GetCheckRunsForRef(ctx context.Context, owner, repo, ref, pat string) ([]CheckRunState, error)
}

type PreviewGetter interface {
    GetPreview(ctx context.Context, owner, repo string, prNumber int) (*PreviewState, error)
}
```

### Assembler

```go
type Assembler struct {
    issues   IssueGetter
    prs      PRGetter
    checks   CheckRunGetter
    previews PreviewGetter
}

func NewAssembler(issues IssueGetter, prs PRGetter, checks CheckRunGetter, previews PreviewGetter) *Assembler

func (a *Assembler) ForIssue(ctx context.Context, owner, repo, pat string, number int) (*FeatureSnapshot, error)
func (a *Assembler) ForPR(ctx context.Context, owner, repo, pat string, prNumber, linkedIssue int) (*FeatureSnapshot, error)
```

`ForPR` logic:
1. Fetch PR details via `prs.GetPR`
2. If `linkedIssue > 0`: fetch issue via `issues.GetIssue` (soft failure -- log warning, continue with nil)
3. Fetch check runs via `checks.GetCheckRunsForRef` using PR's HeadSHA
4. Derive `CIStatus`: any failure -> `CIFailing`, any in_progress/queued -> `CIPending`, all success -> `CIPassing`, empty -> `CIUnknown`
5. Fetch preview via `previews.GetPreview`
6. Return assembled `FeatureSnapshot`

`ForIssue` logic: fetch issue only, everything else nil/unknown.

### Adapters in `adapters.go`

Bridge existing clients to featurecontext interfaces:

```go
type GitHubIssueAdapter struct{ client *githubdomain.Client }
// Wraps client.GetIssue -> maps githubdomain.IssueDetail to featurecontext.IssueState

type GitHubPRAdapter struct{ client *githubdomain.Client }
// Wraps client.GetPR -> maps githubdomain.PRDetail to featurecontext.PRState

type GitHubCheckRunAdapter struct{ client *githubdomain.Client }
// Wraps client.GetCheckRunsForRef -> maps []githubdomain.CheckRun to []featurecontext.CheckRunState

type PreviewAdapter struct{ repo previewdomain.Repository }
// Wraps repo.FindByRepoPR -> maps *previewdomain.Preview to *featurecontext.PreviewState
```

### TDD cycles (mocks in `assembler_test.go`)

5. **`TestAssembler_ForPR_FullContext`** -- All sources return data. PR #10, issue #5, 2 check runs (1 pass, 1 fail), preview ready. Assert: snapshot.PR.Number==10, snapshot.Issue.Number==5, CIStatus==CIFailing, Preview.Status=="ready", len(CIDetails)==2.
6. **`TestAssembler_ForPR_NoLinkedIssue`** -- linkedIssue=0. Assert snapshot.Issue is nil, PR populated.
7. **`TestAssembler_ForPR_NoPreview`** -- PreviewGetter returns nil. Assert snapshot.Preview is nil, no error.
8. **`TestAssembler_ForPR_NoCheckRuns`** -- Empty check runs. Assert CIStatus==CIUnknown.
9. **`TestAssembler_ForIssue_Basic`** -- Issue #42 open. Assert snapshot.Issue populated, PR/CI/Preview nil/unknown.
10. **`TestAssembler_ForPR_IssueGetterError_Nonfatal`** -- IssueGetter returns error. Assert snapshot.Issue is nil, no error returned, PR still populated.
11. **`TestAssembler_CIStatus_AllPassing`** -- All check runs conclusion=="success". Assert CIPassing.
12. **`TestAssembler_CIStatus_Pending`** -- One check run status=="in_progress". Assert CIPending.
13. **`TestAssembler_CIStatus_MixedFailAndPending`** -- One failing, one pending. Assert CIFailing (failure takes precedence).

---

## Phase 3: Context-Aware Message Generation

Replace `formatParentMessage`/`formatEventMessage` with a `MessageGenerator` that produces conversational PM-style messages using feature context.

### Files

| Action | File |
|--------|------|
| Create | `internal/slack/domain/message_generator.go` |
| Create | `internal/slack/domain/message_generator_test.go` |

### Type

```go
type MessageGenerator struct{}

func NewMessageGenerator() *MessageGenerator

func (g *MessageGenerator) ParentMessage(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot) MessageBlock
func (g *MessageGenerator) EventMessage(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot) MessageBlock
```

Both methods handle `snap == nil` gracefully (fallback to basic info from the event itself).

### Message tone examples

| Event | Current | New |
|-------|---------|-----|
| Parent (issue) | `*Issue #42* -- Add dark mode\nby @alice\n\n` `` ``` `` `desc` `` ``` `` `\n<url\|View on GitHub>` | `*#42 Add dark mode*\nOpened by @alice\n\n> desc\n\n<url\|GitHub>` |
| PR opened | `-----\nOpened by @alice` | `@alice opened a pull request for this, touching 50 lines across 3 files.\n<url\|View PR>` |
| Preview ready | `-----\n*Preview ready*\n<url\|Open Preview>` | `The preview is live -- you can try it out here:\n<url\|Open Preview>  \|  <logs\|Logs>` |
| Preview failed | `-----\n*Preview failed*\n> Stage: \`build\`` | `The preview failed during the build step. Check the <logs\|logs> for details, or ask me to investigate.` |
| Comment added | `-----\n*@bob* commented:\n` `` ``` `` `text` `` ``` `` `\n<url\|View on GitHub>` | `@bob commented on GitHub:\n> text\n<url\|View comment>` |
| PR merged (CI known) | `-----\n*Merged* -- Preview will be removed shortly` | `This PR has been merged. CI was passing on the final commit. The preview will be torn down shortly.` |
| PR merged (no CI) | same | `This PR has been merged. The preview will be torn down shortly.` |
| Issue closed (with PR) | `-----\n*Closed*` | `This issue is now closed. It was addressed by PR #52, which has been merged.` |
| Issue closed (no PR) | same | `This issue has been closed.` |
| CI failed (new) | N/A | `CI failed on the latest push. The \`build\` job failed:\n> error text\n<url\|View run>` |
| CI passed (new) | N/A | `CI checks are passing on the latest push.` |

### TDD cycles

14. **`TestMessageGenerator_ParentMessage_Issue`** -- EventIssueOpened, issue #42, author "alice", body "Please add dark mode." Assert: contains "#42", contains "alice", body in blockquote (not code block), GitHub link. No "-----".
15. **`TestMessageGenerator_ParentMessage_PR`** -- EventPROpened, PR #10 by "alice". Snapshot has PR with Additions=50. Assert: mentions "pull request", mentions "alice", mentions line count.
16. **`TestMessageGenerator_EventMessage_PROpened`** -- Snapshot has PR with Additions=50, Deletions=10. Assert: "@alice opened a pull request", line counts, no separator.
17. **`TestMessageGenerator_EventMessage_PreviewReady`** -- EventPRReady, Preview URL set. Assert: "preview is live", contains URL link, contains logs link.
18. **`TestMessageGenerator_EventMessage_PreviewFailed`** -- EventPRFailed, Status="build: exit 1". Assert: "failed during", stage mentioned, logs link, "ask me to investigate".
19. **`TestMessageGenerator_EventMessage_CommentAdded`** -- Author "bob", body "Looks good!" Assert: "@bob commented on GitHub:", body in blockquote (not code block), "View comment" link.
20. **`TestMessageGenerator_EventMessage_PRMerged_WithCI`** -- Snapshot CIStatus==CIPassing. Assert: "merged", "CI was passing".
21. **`TestMessageGenerator_EventMessage_PRMerged_NoCIInfo`** -- Snapshot CIStatus==CIUnknown. Assert: "merged", no CI mention.
22. **`TestMessageGenerator_EventMessage_IssueClosed_WithPR`** -- Snapshot has PR #52 (merged). Assert: "closed", "addressed by PR #52", "merged".
23. **`TestMessageGenerator_EventMessage_IssueClosed_NoPR`** -- Snapshot PR is nil. Assert: "closed", no PR mention.
24. **`TestMessageGenerator_EventMessage_CIFailed`** -- EventCIFailed, CheckRunName="build", FailureSummary="expected 3 got 5". Assert: "CI failed", check name, failure summary in blockquote, workflow URL link.
25. **`TestMessageGenerator_EventMessage_CIPassed`** -- EventCIPassed, CheckRunName="build". Assert: "passing" or similar.
26. **`TestMessageGenerator_NilSnapshot_Fallback`** -- Various events with nil snapshot. Assert: reasonable messages using event data only, no panic.

---

## Phase 4: Notification Overhaul

Wire `MessageGenerator` and `Assembler` into the `Notifier`, replacing old formatting.

### Files

| Action | File |
|--------|------|
| Modify | `internal/slack/domain/notifier.go` |
| Modify | `internal/slack/domain/notifier_test.go` |
| Delete | `formatParentMessage` and `formatEventMessage` functions |

### Structural changes to `Notifier`

```go
type FeatureContextAssembler interface {
    ForPR(ctx context.Context, owner, repo, pat string, prNumber, linkedIssue int) (*featurecontext.FeatureSnapshot, error)
    ForIssue(ctx context.Context, owner, repo, pat string, number int) (*featurecontext.FeatureSnapshot, error)
}

type Notifier struct {
    client     SlackClient
    repository ThreadRepository
    debouncer  Debouncer
    assembler  FeatureContextAssembler  // NEW
    messages   *MessageGenerator        // NEW
    pending    map[string]*pendingFlush
    reactions  map[string]string
    retryWait  time.Duration
    mu         sync.Mutex
}

func NewNotifier(
    client SlackClient,
    repository ThreadRepository,
    debouncer Debouncer,
    assembler FeatureContextAssembler,
) *Notifier
```

### Changes in `flush()`

Before formatting, assemble context:

```go
var snap *featurecontext.FeatureSnapshot
if event.IsPR() {
    snap, _ = n.assembler.ForPR(ctx, event.RepoOwner, event.RepoName, target.GitHubPAT, event.IssueNumber, event.LinkedIssueNumber)
} else {
    snap, _ = n.assembler.ForIssue(ctx, event.RepoOwner, event.RepoName, target.GitHubPAT, event.IssueNumber)
}
```

Then use `n.messages.ParentMessage(event, snap)` / `n.messages.EventMessage(event, snap)` instead of the old functions.

The `target.GitHubPAT` is already available in `flush` via its parameter.

### TDD cycles (add `mockAssembler` to test file)

27. **`TestNotifier_NewThread_UsesMessageGenerator`** -- EventIssueOpened, mock assembler returns snapshot. Assert posted message text matches new conversational format (no "-----", has blockquote body).
28. **`TestNotifier_ExistingThread_UsesMessageGenerator`** -- Pre-populate thread, EventCommentAdded. Assert reply text in new format.
29. **`TestNotifier_AssemblerError_FallsBackGracefully`** -- Assembler returns error. Assert message still posted (nil snapshot triggers fallback formatting).
30. **`TestNotifier_PREvent_PassesLinkedIssueToAssembler`** -- EventPROpened, LinkedIssueNumber=51. Assert assembler's ForPR received linkedIssue=51.
31. **`TestNotifier_IssueEvent_CallsForIssue`** -- EventIssueOpened. Assert assembler's ForIssue called (not ForPR).

All existing notifier tests must be updated: change `NewNotifier` calls to include mock assembler, update message text assertions to match new format.

### Wiring in `main.go`

```go
featureAssembler := featurecontext.NewAssembler(
    featurecontext.NewGitHubIssueAdapter(githubClient),
    featurecontext.NewGitHubPRAdapter(githubClient),
    featurecontext.NewGitHubCheckRunAdapter(githubClient),
    featurecontext.NewPreviewAdapter(previewRepo),
)

slackNotifier := slackdomain.NewNotifier(slackClient, slackRepo, slackDebouncer, featureAssembler)
```

---

## Phase 5: Agent Context Enrichment

Give the agent pre-loaded feature state so it doesn't need tool calls to understand the current situation.

### Files

| Action | File |
|--------|------|
| Modify | `internal/agent/domain/models.go` |
| Modify | `internal/agent/domain/context.go` |
| Modify | `internal/agent/domain/context_test.go` |
| Modify | `internal/agent/domain/specialist.go` |
| Modify | `internal/slack/web/handlers.go` |
| Create | `internal/slack/web/snapshot_formatter.go` |
| Create | `internal/slack/web/snapshot_formatter_test.go` |

### Changes to `models.go`

Add field to `RunRequest`:

```go
type RunRequest struct {
    // ...existing fields...
    FeatureSummary string // Pre-formatted feature context summary for LLM
}
```

### Changes to `context.go`

```go
func BuildContext(
    systemPrompt string,
    userMessage string,
    threadMessages []ThreadMessage,
    linkedIssue *IssueContext,
    featureSummary string,           // NEW
    budget TokenBudget,
) []Message
```

When `featureSummary != ""`, inject it as a system message between thread history and user message:

```
[Feature status]
Issue: #42 Add dark mode (open)
PR: #10 by alice -- open, +50/-10 lines, branch feature-branch
CI: build passing, lint failing
Preview: live at https://preview.example.com
```

Token budget checked before inclusion (same pattern as linkedIssue).

### Changes to `specialist.go`

Update the `BuildContext` call at line 85:

```go
messages := BuildContext(systemPrompt, userMessage, threadMsgs, req.LinkedIssue, req.FeatureSummary, s.tokenBudget)
```

### Snapshot formatter (`slack/web/snapshot_formatter.go`)

```go
func FormatFeatureSummary(snap *featurecontext.FeatureSnapshot) string
```

Produces the compact multi-line summary string. Only includes non-empty fields.

### Changes to Slack handler (`slack/web/handlers.go`)

Add `FeatureContextAssembler` interface and field to `Handler`. After resolving `linkedIssue` (line ~233-246), assemble context and format:

```go
var featureSummary string
if h.featureAssembler != nil && threadTs != "" {
    if thread, _ := h.threadFinder.FindThreadBySlackTs(ctx, threadTs); thread != nil {
        var snap *featurecontext.FeatureSnapshot
        if thread.GithubPRID > 0 {
            snap, _ = h.featureAssembler.ForPR(ctx, target.RepoOwner, target.RepoName, target.GitHubPAT, thread.GithubPRID, thread.GithubIssueID)
        } else if thread.GithubIssueID > 0 {
            snap, _ = h.featureAssembler.ForIssue(ctx, target.RepoOwner, target.RepoName, target.GitHubPAT, thread.GithubIssueID)
        }
        if snap != nil {
            featureSummary = FormatFeatureSummary(snap)
        }
    }
}

req := agent.RunRequest{
    // ...existing fields...
    FeatureSummary: featureSummary,
}
```

### TDD cycles

32. **`TestBuildContext_FeatureSummary_Included`** -- Non-empty summary string. Assert messages contain system message with "Feature status" and the summary text, positioned between thread history and user message.
33. **`TestBuildContext_FeatureSummary_Empty`** -- Empty string. Assert no extra system message added.
34. **`TestBuildContext_FeatureSummary_TokenBudget`** -- Tight budget. Assert summary dropped when budget exhausted, system prompt and user message retained.
35. **`TestFormatFeatureSummary_Full`** -- Snapshot with issue, PR, CI failing, preview ready. Assert output contains all four sections.
36. **`TestFormatFeatureSummary_IssueOnly`** -- Only issue set. Assert only issue line present, no empty "PR:" or "CI:" lines.
37. **`TestFormatFeatureSummary_Nil`** -- Nil snapshot. Assert returns empty string.
38. **`TestSpecialist_PassesFeatureSummary`** -- RunRequest with FeatureSummary set. Mock LLM returns text. Assert ChatRequest messages contain the summary.

Update all existing `BuildContext` test calls to pass the new parameter (empty string to preserve behavior).

### Wiring in `main.go`

Pass `featureAssembler` (already built in Phase 4) to the Slack handler. Add a setter or constructor parameter on `slackweb.Handler`.

---

## Phase 6: CI Webhook Handling

Handle `check_run` events from GitHub to notify about CI pass/fail.

### Files

| Action | File |
|--------|------|
| Modify | `internal/slack/facade/dto.go` |
| Modify | `internal/github/domain/webhook.go` |
| Modify | `internal/github/domain/webhook_test.go` |
| Modify | `internal/github/web/handlers.go` |
| Modify | `internal/github/web/handlers_test.go` |

### New event types in `dto.go`

```go
EventCIFailed  EventType = "ci_failed"
EventCIPassed  EventType = "ci_passed"
```

New fields on `NotificationEvent`:

```go
CheckRunName    string // "build", "lint", etc.
FailureSummary  string // extracted error text
WorkflowURL     string // link to the check run
HeadSHA         string // commit that triggered the check
```

### New event parsing in `webhook.go`

```go
type CheckRunEvent struct {
    Action   string          `json:"action"`
    CheckRun CheckRunPayload `json:"check_run"`
    Repository struct {
        Owner struct { Login string `json:"login"` } `json:"owner"`
        Name  string `json:"name"`
    } `json:"repository"`
}

type CheckRunPayload struct {
    ID           int64  `json:"id"`
    Name         string `json:"name"`
    Status       string `json:"status"`
    Conclusion   string `json:"conclusion"`
    HTMLURL      string `json:"html_url"`
    HeadSHA      string `json:"head_sha"`
    PullRequests []struct {
        Number int `json:"number"`
    } `json:"pull_requests"`
}

func ParseCheckRunEvent(payload []byte) (*CheckRunEvent, error)
```

### New handler method in `github/web/handlers.go`

Add `"check_run"` case to `HandleWebhook` switch. New method:

```go
func (h *Handler) handleCheckRun(w http.ResponseWriter, r *http.Request, body []byte)
```

Logic:
1. Parse event
2. Skip if `action != "completed"`
3. Skip if `len(PullRequests) == 0` (only notify for PR-linked checks)
4. Look up target by repo
5. Validate signature
6. For each linked PR: create NotificationEvent with EventCIFailed or EventCIPassed
7. Call `h.notifier.Notify()`

### TDD cycles

39. **`TestParseCheckRunEvent_Failure`** -- Parse payload with conclusion="failure", name="build", head_sha="abc", pull_requests=[{number:10}]. Assert correct parsing.
40. **`TestParseCheckRunEvent_Success`** -- conclusion="success". Assert correct parsing.
41. **`TestHandleWebhook_CheckRun_Failure_NotifiesSlack`** -- Send check_run webhook, conclusion="failure". Assert mock notifier receives EventCIFailed with correct CheckRunName and IssueNumber (PR number).
42. **`TestHandleWebhook_CheckRun_Success_NotifiesSlack`** -- conclusion="success". Assert EventCIPassed.
43. **`TestHandleWebhook_CheckRun_InProgress_Ignored`** -- action="created". Assert no notification.
44. **`TestHandleWebhook_CheckRun_NoPR_Ignored`** -- Empty pull_requests. Assert no notification.

Message generator tests for CI events already covered in Phase 3 (tests 24-25).

---

## Phase 7: PR Comment Reduction

Minimize PR comments when Slack is tracking the feature.

### Files

| Action | File |
|--------|------|
| Modify | `internal/preview/domain/interfaces.go` |
| Modify | `internal/preview/domain/service.go` |
| Modify | `internal/preview/domain/service_test.go` |
| Modify | `cmd/server/main.go` |

### New interface in `interfaces.go`

```go
type SlackThreadChecker interface {
    HasThread(ctx context.Context, repoOwner, repoName string, prNumber int) bool
}
```

### Changes to `Service`

Add optional dependency via functional option:

```go
type ServiceOption func(*Service)

func WithSlackThreadChecker(checker SlackThreadChecker) ServiceOption {
    return func(s *Service) { s.slackThreadChecker = checker }
}
```

In `DeployPreview`, before building progress comments:
- If `s.slackThreadChecker != nil` and `HasThread()` returns true: post a single minimal comment (`"Preview deploying -- status tracked in Slack. <!-- productbuilding-orchestrator -->"`) and update it to `"Preview: {url} <!-- productbuilding-orchestrator -->"` when ready.
- Otherwise: keep existing detailed progress checklist.

### TDD cycles

45. **`TestService_DeployPreview_SlackThread_MinimalComment`** -- SlackThreadChecker returns true. Assert comment body contains "tracked in Slack", does NOT contain progress checklist steps.
46. **`TestService_DeployPreview_NoSlackThread_FullComment`** -- Checker returns false. Assert full progress checklist.
47. **`TestService_DeployPreview_NilChecker_FullComment`** -- Checker is nil. Assert full progress checklist (nil-safe).
48. **`TestService_PreviewReady_SlackThread_MinimalFinalComment`** -- Checker returns true, preview reaches ready. Assert final comment has preview URL but no full checklist.

### Wiring in `main.go`

```go
type slackThreadCheckerAdapter struct{ repo slackinfra.ThreadRepository }

func (a *slackThreadCheckerAdapter) HasThread(ctx context.Context, owner, repo string, prNumber int) bool {
    thread, err := a.repo.FindThreadByPR(ctx, owner, repo, prNumber)
    return err == nil && thread != nil
}

previewService := previewdomain.NewService(
    // ...existing args...,
    previewdomain.WithSlackThreadChecker(&slackThreadCheckerAdapter{slackRepo}),
)
```

---

## Complete File Inventory

### New files (7)

1. `internal/featurecontext/assembler.go`
2. `internal/featurecontext/assembler_test.go`
3. `internal/featurecontext/adapters.go`
4. `internal/slack/domain/message_generator.go`
5. `internal/slack/domain/message_generator_test.go`
6. `internal/slack/web/snapshot_formatter.go`
7. `internal/slack/web/snapshot_formatter_test.go`

### Modified files (17)

1. `internal/github/domain/client.go` -- GetPR, GetCheckRunsForRef
2. `internal/github/domain/client_test.go`
3. `internal/github/domain/webhook.go` -- CheckRunEvent, ParseCheckRunEvent
4. `internal/github/domain/webhook_test.go`
5. `internal/github/web/handlers.go` -- handleCheckRun, check_run case
6. `internal/github/web/handlers_test.go`
7. `internal/slack/facade/dto.go` -- new event types and fields
8. `internal/slack/domain/notifier.go` -- assembler + message generator deps
9. `internal/slack/domain/notifier_test.go` -- updated for new constructor + message format
10. `internal/agent/domain/models.go` -- FeatureSummary on RunRequest
11. `internal/agent/domain/context.go` -- featureSummary parameter
12. `internal/agent/domain/context_test.go`
13. `internal/agent/domain/specialist.go` -- pass FeatureSummary to BuildContext
14. `internal/slack/web/handlers.go` -- feature assembler integration
15. `internal/preview/domain/service.go` -- minimal comment logic
16. `internal/preview/domain/interfaces.go` -- SlackThreadChecker
17. `cmd/server/main.go` -- wire assembler, adapters, checker

## Verification

After all phases:

1. `mise run app-tests` -- all unit tests pass (existing + 48 new)
2. `mise run app-quality` -- go vet + gofmt clean
3. `mise run app-build` -- compiles cleanly
4. Manual test: trigger a GitHub issue creation webhook, verify Slack gets conversational parent message
5. Manual test: trigger PR opened webhook with "Fixes #N", verify reply in existing issue thread with new tone
6. Manual test: @mention the bot in a feature thread, verify agent response includes feature state context
7. Manual test: trigger a check_run failure webhook for a PR, verify Slack thread gets CI failure notification
