# Fix six production issues in Slack-GitHub integration

## Context

Mental execution of real-world use cases revealed six issues — ranging from a merged PR always showing as "closed" in Slack, to a race condition that creates duplicate threads when the agent creates GitHub issues. This plan fixes all six in dependency order, using strict red-green TDD throughout.

## Implementation order

Issues are ordered so earlier fixes don't conflict with later ones:

1. **Issue 1** — PR merged shows as "closed" (webhook parsing + handler)
2. **Issue 5** — Orphan threads from CI/preview events (notifier guard)
3. **Issue 2** — "Issue closed, addressed by PR" unreachable (notifier assembler enrichment)
4. **Issue 4** — UserNote never reaches Slack (preview service plumbing)
5. **Issue 6** — Duplicate FindThreadBySlackTs calls (Slack handler cache)
6. **Issue 3** — Agent-created issues race condition (tool executor callback)

---

## Issue 1: PR merges always show as "closed"

**Root cause:** `webhookPayload` doesn't parse `pull_request.merged`. `PREvent` has no `Merged` field. Handler always emits `EventPRClosed` for `action:"closed"`.

### Files

| Action | File |
|--------|------|
| Modify | `internal/github/domain/webhook.go` |
| Modify | `internal/github/domain/webhook_test.go` |
| Modify | `internal/github/web/handlers.go` |
| Modify | `internal/github/web/handlers_test.go` |

### Changes

**webhook.go** — Add `Merged bool` to `webhookPayload.PullRequest` struct (~line 47) and to `PREvent` struct (~line 33). Set it in `ParsePREvent`.

**handlers.go** — In `handlePullRequest`, replace the `case "closed"` block (~line 121) with:

```go
case "closed":
    go h.previewService.DeletePreview(context.Background(), req, target.GitHubPAT)
    if h.notifier != nil {
        eventType := slackfacade.EventPRClosed
        if event.Merged {
            eventType = slackfacade.EventPRMerged
        }
        go h.notifySlackPR(eventType, event, target)
    }
```

### TDD cycles

1. **RED:** `TestParsePREvent_Merged` — payload with `"merged": true`. Assert `event.Merged == true`.
   **GREEN:** Add `Merged` to both structs, set in `ParsePREvent`.

2. **RED:** `TestParsePREvent_ClosedNotMerged` — payload with `"merged": false`. Assert `event.Merged == false`.
   **GREEN:** Already passes.

3. **RED:** `TestHandleWebhook_PRMerged_NotifiesSlack` — send `action:"closed"` + `merged:true` webhook. Assert notifier receives `EventPRMerged` (not `EventPRClosed`). Follow existing pattern from `TestHandleWebhook_PROpened_PassesLinkedIssueNumber` — use raw `map[string]interface{}` payload, `generateSignature`, async wait.
   **GREEN:** Add the `if event.Merged` check in the handler.

4. **RED:** `TestHandleWebhook_PRClosedNotMerged_NotifiesSlack` — send `action:"closed"` + `merged:false`. Assert `EventPRClosed`.
   **GREEN:** Already passes with the else branch.

---

## Issue 5: CI/preview events creating orphan threads

**Root cause:** When the notifier can't find a thread for a non-creation event (e.g. `EventCIFailed`, `EventPRReady`), it creates a new parent thread with broken content and loses the actual event message (skipped because `newThread == true`).

### Files

| Action | File |
|--------|------|
| Modify | `internal/slack/domain/notifier.go` |
| Modify | `internal/slack/domain/notifier_test.go` |

### Changes

**notifier.go** — In `flush()`, just before the `newThread := false` / `if thread == nil` block (~line 206), add a guard: if no thread was found and the event is NOT a thread-creating event (`EventIssueOpened`, `EventPROpened`), skip creating a new thread — log a warning and fall through to the comment phase.

```go
// Only EventIssueOpened and EventPROpened should create new threads.
// Other events (CI, preview, close, merge) are only meaningful as
// replies in existing threads — skip them if no thread exists.
if thread == nil && event.Type != slackfacade.EventIssueOpened && event.Type != slackfacade.EventPROpened {
    slog.Info("skipping notification: no existing thread for non-creation event",
        "event", event.Type,
        "repo", event.RepoOwner+"/"+event.RepoName,
        "number", event.IssueNumber,
    )
    // Fall through — comments in phase 2 will also be skipped
    // since thread remains nil.
    p.status = nil
}
```

Place this after the retry block (~line 203) and before `newThread := false` (~line 206). Setting `p.status = nil` causes the rest of phase 1 to be skipped (guarded by `if p.status != nil`). Phase 2 handles `thread == nil` by logging and returning.

### TDD cycles

5. **RED:** `TestNotifier_CIFailed_NoThread_SkipsNotification` — send `EventCIFailed` for PR #10 with no pre-existing thread. Assert: no message posted to Slack (mock client's `postedMessages` is empty).
   **GREEN:** Add the guard.

6. **RED:** `TestNotifier_PreviewReady_NoThread_SkipsNotification` — same for `EventPRReady`.
   **GREEN:** Already passes with the guard.

7. **RED:** `TestNotifier_PRMerged_NoThread_SkipsNotification` — same for `EventPRMerged`.
   **GREEN:** Already passes.

8. **RED:** `TestNotifier_IssueOpened_NoThread_StillCreatesThread` — confirm `EventIssueOpened` still creates a new thread when none exists (regression guard).
   **GREEN:** Already passes — guard doesn't apply to creation events.

---

## Issue 2: "Issue closed, addressed by PR" unreachable

**Root cause:** The notifier calls `ForIssue()` for issue events, which only fetches issue data — `snap.PR` is always nil. But the thread mapping may have `GithubPRID > 0` from a linked PR.

### Files

| Action | File |
|--------|------|
| Modify | `internal/slack/domain/notifier.go` |
| Modify | `internal/slack/domain/notifier_test.go` |

### Changes

**notifier.go** — In the assembler block of `flush()` (~lines 132-149), for non-PR events, do a preliminary thread lookup to check for a linked PR. If the thread has `GithubPRID > 0`, call `ForPR` instead of `ForIssue`:

```go
if refEvent.IsPR() {
    snap, _ = n.assembler.ForPR(ctx, refEvent.RepoOwner, refEvent.RepoName, target.GitHubPAT, refEvent.IssueNumber, refEvent.LinkedIssueNumber)
} else {
    // Check if thread has a linked PR (e.g., issue closed after PR merged)
    if t, _ := n.repository.FindThreadByNumber(ctx, refEvent.RepoOwner, refEvent.RepoName, refEvent.IssueNumber); t != nil && t.GithubPRID > 0 {
        snap, _ = n.assembler.ForPR(ctx, refEvent.RepoOwner, refEvent.RepoName, target.GitHubPAT, t.GithubPRID, refEvent.IssueNumber)
    } else {
        snap, _ = n.assembler.ForIssue(ctx, refEvent.RepoOwner, refEvent.RepoName, target.GitHubPAT, refEvent.IssueNumber)
    }
}
```

One extra SQLite lookup per issue event flush — microseconds.

### TDD cycles

9. **RED:** `TestNotifier_IssueClosed_WithLinkedPR_UsesForPR` — pre-populate thread with `GithubIssueID=42, GithubPRID=52`. Mock assembler returns snapshot with `PR: {Number: 52, Merged: true}`. Send `EventIssueClosed` for issue #42. Assert: mock assembler's `forPRCalls` has one entry with `PRNumber=52, LinkedIssue=42`.
   **GREEN:** Add the thread lookup in the assembler block.

10. **RED:** `TestNotifier_IssueClosed_WithLinkedPR_MessageShowsPR` — same setup. Assert posted message contains "addressed by PR #52" and "merged".
    **GREEN:** Already passes once ForPR is called correctly (the message generator's `eventIssueClosed` already handles `snap.PR != nil && snap.PR.Merged`).

11. **RED:** `TestNotifier_IssueClosed_NoLinkedPR_UsesForIssue` — thread with `GithubPRID=0`. Assert assembler's `forIssueCalls` has one entry (not `forPRCalls`).
    **GREEN:** Already passes — the else branch.

---

## Issue 4: UserNote from preview contract never reaches Slack

**Root cause:** `notifySlack` in preview service doesn't set `UserNote` on the notification event. The contract's `UserFacingNote` is available in `DeployPreview` scope but not plumbed through.

### Files

| Action | File |
|--------|------|
| Modify | `internal/preview/domain/service.go` |
| Modify | `internal/preview/domain/service_test.go` |
| Modify | `internal/slack/domain/message_generator_test.go` |

### Changes

**service.go** — Add `userNote string` parameter to `notifySlack` method signature (~line 503). Set `event.UserNote = userNote` in the event construction (~line 525). Update all call sites:
- `notifySlack(ctx, &preview, EventPRReady, "ready", target)` → add `meta.UserFacingNote` as the last arg
- `notifySlack(ctx, p, EventPRFailed, stage+": "+message, target)` → pass `""` (no note on failure)

### TDD cycles

12. **RED:** `TestDeployPreview_ReadyNotification_IncludesUserNote` — use `contractWithUserNote()` helper (new) that returns YAML with `user_facing_note: "Login with test@example.com"`. Run `DeployPreview`. Assert mock notifier received event with `UserNote == "Login with test@example.com"`.
    **GREEN:** Add `userNote` param to `notifySlack`, pass `meta.UserFacingNote` at the ready call site.

13. **RED:** `TestDeployPreview_FailedNotification_NoUserNote` — inject failure. Assert notifier received event with `UserNote == ""`.
    **GREEN:** Already passes — failure call site passes `""`.

14. **RED:** `TestMessageGenerator_EventMessage_PreviewReady_WithUserNote` — event with `UserNote: "Use test@example.com"`. Assert message contains `> *Note:*` and the note text.
    **GREEN:** Already passes — the message generator code at `message_generator.go:152` already handles this. This test documents that the path is now reachable.

---

## Issue 6: FindThreadBySlackTs called twice per @mention

**Root cause:** In `handleAppMention` (`slack/web/handlers.go`), `FindThreadBySlackTs` is called at ~line 249 (for `linkedIssue`) and again at ~line 264 (for `featureSummary`).

### Files

| Action | File |
|--------|------|
| Modify | `internal/slack/web/handlers.go` |
| Modify | `internal/slack/web/handlers_test.go` |

### Changes

**handlers.go** — In `handleAppMention`, hoist the thread lookup before both consumers. Replace the two separate lookups with a single one:

```go
// Look up thread once (used for both linked issue and feature context)
var thread *domain.SlackThread
if threadTs != "" {
    thread, _ = h.threadFinder.FindThreadBySlackTs(ctx, threadTs)
}

// Use thread for linked issue
if thread != nil {
    linkedIssue = &agent.IssueContext{Number: thread.GithubIssueID}
    if thread.GithubPRID > 0 {
        linkedIssue.Number = thread.GithubPRID
    }
}

// Use same thread for feature context
if h.featureAssembler != nil && thread != nil {
    // ... assemble using thread.GithubPRID / thread.GithubIssueID
}
```

### TDD cycles

15. **RED:** `TestHandler_AppMention_ThreadLookedUpOnce` — add `callCount int` to `mockThreadFinder`. Send an `app_mention` event in a thread. Assert `mockThreadFinder.callCount == 1`.
    **GREEN:** Hoist the lookup.

16. **RED:** `TestHandler_AppMention_NoThread_StillWorks` — `mockThreadFinder` returns nil. Assert agent still runs, `FeatureSummary` is empty, `LinkedIssue` is nil.
    **GREEN:** Already passes — nil checks handle this.

---

## Issue 3: Agent-created issues race condition

**Root cause:** When the agent creates a GitHub issue, the webhook fires immediately, but the Slack handler saves the thread mapping only after the agent finishes (10-120s later). The notifier's 5s retry is too short, so it creates a duplicate orphan thread.

### Files

| Action | File |
|--------|------|
| Modify | `internal/agent/domain/tools.go` |
| Modify | `internal/agent/domain/models.go` |
| Modify | `internal/agent/domain/specialist.go` |
| Modify | `internal/agent/domain/test_helpers_test.go` |
| Modify | `internal/slack/web/handlers.go` |
| Modify | `internal/slack/web/handlers_test.go` |

### Changes

**models.go** — Add `OnIssueCreated` callback directly to `RunRequest`:

```go
type RunRequest struct {
    // ...existing fields...
    OnIssueCreated func(owner, repo string, number int, title string)
}
```

**tools.go** — Add `onIssueCreated` field and setter on `GitHubToolExecutor`:

```go
func (e *GitHubToolExecutor) SetOnIssueCreated(fn func(owner, repo string, number int, title string)) {
    e.onIssueCreated = fn
}
```

In `createIssue()` at ~line 180, after appending to `e.effects.CreatedIssues`, fire the callback:

```go
e.effects.CreatedIssues = append(e.effects.CreatedIssues, CreatedIssue{Number: number, Title: args.Title})
if e.onIssueCreated != nil {
    e.onIssueCreated(target.RepoOwner, target.RepoName, number, args.Title)
}
```

Add `SetOnIssueCreated` to the `ToolExecutor` interface.

**specialist.go** — Before the LLM loop, pass the callback from the request to the tool executor:

```go
if req.OnIssueCreated != nil {
    s.tools.SetOnIssueCreated(req.OnIssueCreated)
}
```

**handlers.go** — In `handleAppMention`, set the callback when building the agent request:

```go
req := agent.RunRequest{
    // ...existing fields...
    OnIssueCreated: func(owner, repo string, number int, title string) {
        thread, err := domain.NewSlackThread(owner, repo, number, 0, event.Channel, replyTs)
        if err != nil {
            slog.Warn("failed to create early thread mapping", "error", err)
            return
        }
        if err := h.threadSaver.SaveThread(ctx, thread); err != nil {
            slog.Warn("failed to save early thread mapping", "error", err, "issue", number)
        } else {
            slog.Info("saved early thread mapping", "issue", number, "thread_ts", replyTs)
        }
    },
}
```

Keep the existing post-run thread-saving loop as a safety net — `SaveThread` on an existing mapping is idempotent.

### TDD cycles

17. **RED:** `TestToolExecutor_CreateIssue_FiresCallback` — set `OnIssueCreated` callback on the executor via `SetOnIssueCreated`. Mock GitHub client returns issue #42. Execute `create_github_issue`. Assert callback fired with correct owner, repo, number, title.
    **GREEN:** Add the `onIssueCreated` field and call it in `createIssue()`.

18. **RED:** `TestToolExecutor_CreateIssue_NilCallback_NoPanic` — don't set callback. Execute `create_github_issue`. Assert no panic.
    **GREEN:** Already passes — guarded by `if e.onIssueCreated != nil`.

19. **RED:** `TestToolExecutor_SetOnIssueCreated` — call `SetOnIssueCreated`, verify the function is stored.
    **GREEN:** Add the setter.

20. **RED:** `TestSpecialist_PassesOnIssueCreatedToTools` — set `OnIssueCreated` on `RunRequest`. Mock LLM returns a `create_github_issue` tool call. Assert the callback fires.
    **GREEN:** Add the `SetOnIssueCreated` call in specialist before the LLM loop.

21. **RED:** `TestHandler_AppMention_IssueCreated_SavesThreadImmediately` — mock agent runner that, within `Run()`, synchronously invokes the `OnIssueCreated` callback (simulating tool execution). Assert `mockThreadSaver.getSaved()` contains the issue mapping BEFORE `Run()` returns.
    **GREEN:** Set up the callback in the handler.

---

## Verification

After all changes:

```sh
mise run app-tests      # All tests pass with -race
mise run app-quality    # go vet + gofmt clean
mise run app-build      # Compiles cleanly
```

Specific package runs for fast iteration during development:

```sh
mise run app-exec go test -race -run TestParsePREvent ./internal/github/domain/
mise run app-exec go test -race -run TestHandleWebhook_PR ./internal/github/web/
mise run app-exec go test -race -run TestNotifier_ ./internal/slack/domain/
mise run app-exec go test -race -run TestDeployPreview_ ./internal/preview/domain/
mise run app-exec go test -race -run TestHandler_AppMention ./internal/slack/web/
mise run app-exec go test -race -run TestToolExecutor_ ./internal/agent/domain/
mise run app-exec go test -race -run TestSpecialist_ ./internal/agent/domain/
```
