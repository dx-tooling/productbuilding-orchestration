# Slack Notifier: Two-Lane Event Buffer

The notifier bridges GitHub events to Slack threads. It receives a stream of
`NotificationEvent`s (issue opened, PR ready, comment added, ...) and posts
them into per-issue Slack threads with debouncing, deduplication, and ordering
guarantees.

This document explains the buffering design, thread resolution chain, and the
race conditions it solves.

## Files

| File | Role |
|---|---|
| `facade/dto.go` | `NotificationEvent` struct, event type constants (including `EventCIFailed`/`EventCIPassed`), classification helpers (`IsPR`, `IsComment`) |
| `domain/notifier.go` | `Notifier` — buffering, debouncing, thread resolution, feature context assembly, Slack posting |
| `domain/message_generator.go` | `MessageGenerator` — produces conversational PM-style messages from events + feature snapshots |
| `domain/models.go` | `SlackThread` — the GitHub-issue-to-Slack-thread mapping persisted in SQLite |

## Event lifecycle

```
GitHub webhook
     |
     v
  github/web handler ──> Notify(event, target)
                              |
                         ┌────┴─────┐
                         │  Notifier │
                         │           │
                         │ 1. Buffer │  (under mutex)
                         │ 2. Emoji  │  (immediate, not debounced)
                         │ 3. Debounce(key, 2s, flush)
                         └────┬──────┘
                              │  after 2s quiet period
                              v
                           flush()
                         ┌──────────────┐
                         │ Phase 1:     │  status event (if any)
                         │  resolve     │  → find/create thread
                         │  thread      │  → post status update
                         ├──────────────┤
                         │ Phase 2:     │  comments (if any)
                         │  post each   │  → to resolved thread
                         │  in order    │  → retry if thread pending
                         └──────────────┘
```

## The two-lane buffer

All events for the same issue share a key (`channel#issueNumber`). Within that
key, events are split into two lanes:

```go
type pendingFlush struct {
    status   *NotificationEvent   // latest lifecycle event (overwrite)
    comments []*NotificationEvent // all comments in order (append)
}
```

**Status lane** — lifecycle events like `pr_opened`, `pr_ready`, `pr_merged`,
`issue_closed`. Only the *latest* status survives the debounce window. This
deduplicates rapid state transitions (opened -> ready within 2s posts only
"ready").

**Comment lane** — `comment_added` and `comment_edited` events. Every comment
is preserved in arrival order. No deduplication.

### Why two lanes?

A single-slot buffer (`map[key]*event`) caused three classes of race:

1. **PR opened + comment within 2s** — comment overwrote buffer, PR event lost.
   Thread was never created, comment had nowhere to post.
2. **Multiple comments on same issue** — only the last survived.
3. **Comment before lifecycle event** — comment silently dropped even though the
   lifecycle event arrived 500ms later and would have created the thread.

The two-lane design ensures status events always create/find the thread first,
and all comments are preserved.

## Notify: event routing

```
Notify(event) {
    key = channel#issueNumber

    lock {
        if event.IsComment() → append to pending[key].comments
        else                 → overwrite pending[key].status
    }

    emoji reactions → immediate (swap current reaction)

    debouncer.Debounce(key, 2s, flush)   ← ALL event types
}
```

Key points:
- **All events go through the debouncer**, including comments. This batches
  a status event and its comments into a single flush where ordering is
  guaranteed.
- **Emoji reactions bypass the debouncer** — they're visual-only and benefit
  from instant feedback.

## flush: two-phase processing

Flush atomically grabs the pending entry (grab-and-delete under mutex), then
processes in two phases.

### Phase 1: status event

Resolves a Slack thread through a chain of lookups, then posts the status
update:

```
1. FindThreadByNumber(issueNumber)          — direct match
2. FindThreadByNumber(linkedIssueNumber)    — PR referencing parent issue
   └─ if found & event is PR → save new PR→thread mapping
3. if still nil & event is issue_opened/pr_opened:
   └─ sleep 5s → retry FindThreadByNumber  — race with concurrent handler
4. if still nil → create new thread (PostMessage → save mapping)
5. if thread existed → post status as thread reply
   if thread is new → parent message IS the update (skip reply)
```

The linked-issue fallback (step 2) handles agent-created PRs that reference
`"Fixes #N"` — the PR lands in the parent issue's thread and a new mapping
is saved so future PR events (comments, merges) find the thread directly.

The issue-to-PR upgrade (after step 4) handles the GitHub quirk where PRs and
issues share the numbering space: if a number was first seen as an issue and
now arrives as a PR, the thread's type is updated.

### Phase 2: comments

Posts each queued comment to the thread resolved in phase 1:

```
1. if thread not resolved in phase 1:
   └─ FindThreadByNumber using first comment's details
   └─ if still nil → sleep 5s → retry (concurrent thread creation)
   └─ if still nil → log & skip all comments
2. for each comment in order → PostToThread
```

The retry covers the case where a comment arrives in a separate debounce batch
from the thread-creating event, and the thread creation is still in progress.

## Thread resolution summary

```
                    ┌─ direct match by number ─────────────┐
                    │                                      │
event ──> lookup ───┤                                      ├──> thread
                    ├─ linked issue fallback (#N in body) ─┤
                    │                                      │
                    ├─ 5s retry (for opened events) ───────┤
                    │                                      │
                    └─ create new thread ──────────────────┘
```

For comments without a status event in the same batch:

```
event ──> lookup ───┬─ direct match ──────────────────────> thread
                    │
                    └─ 5s retry ──┬─ found ───────────────> thread
                                  └─ not found ───────────> skip (logged)
```

## Concurrency and safety

- **Mutex (`n.mu`)** guards `pending` and `reactions` maps. Held only during
  map reads/writes, never during I/O.
- **Grab-and-delete** in flush: `p := n.pending[key]; delete(n.pending, key)`
  under lock. Subsequent flush calls for the same key get `nil` and return
  immediately. This makes flush idempotent.
- **Debouncer** ensures at most one flush runs per key at a time. Multiple
  `Debounce()` calls for the same key replace the callback and reset the timer.
- **retryWait** (default 5s) is an internal field, overridable in tests to
  avoid slow sleeps.

## Feature context assembly

Before formatting any message, `flush()` assembles a `FeatureSnapshot` via the
`FeatureContextAssembler` interface:

```
flush() {
    // Determine reference event
    refEvent = status or first comment

    // Assemble context
    if refEvent.IsPR() → assembler.ForPR(owner, repo, pat, prNumber, linkedIssue)
    else               → assembler.ForIssue(owner, repo, pat, issueNumber)

    // Format messages using snapshot
    parentMsg  = messages.ParentMessage(event, snapshot)
    replyMsg   = messages.EventMessage(event, snapshot)
}
```

The assembler is a soft dependency — if it returns an error, `snap` is nil and
the `MessageGenerator` falls back to event-only data. If the assembler itself is
nil (e.g. in tests without feature context), all messages still render correctly.

The `FeatureSnapshot` contains:
- **Issue state**: number, title, body, open/closed
- **PR state**: number, author, additions/deletions, head SHA, merge status
- **CI status**: aggregate (passing/failing/pending/unknown) + per-check details
- **Preview state**: status (ready/building/failed) + URL

## Message generation

The `MessageGenerator` replaced the old `formatParentMessage`/`formatEventMessage`
functions. Key differences from the old format:

- Body text uses blockquotes (`> text`) instead of code blocks (`` ``` ``)
- No thread separators (`─────`)
- Conversational tone ("The preview is live — you can try it out here")
- Context-aware: merged PRs mention CI status, closed issues mention the addressing PR
- CI events (`EventCIFailed`/`EventCIPassed`) format check run names and failure summaries

Both `ParentMessage` and `EventMessage` accept a `*FeatureSnapshot` that can be nil.

## Test matrix

| Test | What it verifies |
|---|---|
| `TwoComments_PreservedInOrder` | Comment lane preserves all comments in order |
| `PROpenedPlusComment_SameBatch` | Status creates thread, comment posts to it |
| `CommentBeforeLifecycle_SameBatch` | Status processed first despite arriving second |
| `StatusDedup_StillWorks` | Rapid status events collapse to latest only |
| `OrphanComment_RetriesGivesUp` | Comment with no thread skipped after retry |
| `OrphanComment_RetriesFindsThread` | Comment finds thread during retry window |
| `FlushIdempotent` | Double flush posts only once |
| `CommentOnUnknownIssue_NoNewThread` | Comment never creates channel-level message |
| `CommentOnKnownIssue_PostsToThread` | Comment on known issue posts to existing thread |
| `Flush_RetriesForNewIssue_FindsThreadMapping` | Status retry finds thread created by concurrent handler |
| `PRLinksToIssueThread_CreatesNewMapping` | Linked-issue fallback + PR mapping |
| `MultiplePRsPerIssue_AllLinkToSameThread` | Multiple PRs on same issue share thread |
| `NewThread_UsesMessageGenerator` | New thread uses conversational format (blockquotes, no separators) |
| `ExistingThread_UsesMessageGenerator` | Thread reply uses new comment format |
| `AssemblerError_FallsBackGracefully` | Assembler failure still posts message (nil snapshot fallback) |
| `PREvent_PassesLinkedIssueToAssembler` | Assembler receives correct linkedIssue from PR event |
| `IssueEvent_CallsForIssue` | Issue events route to ForIssue (not ForPR) |
