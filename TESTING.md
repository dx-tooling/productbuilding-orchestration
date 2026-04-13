# Testing Guide

Every external system integration in this codebase is fully testable through deterministic mocks. No test requires Docker, network access, or real API calls. This document covers the conventions, patterns, and infrastructure that make this work.

## Running tests

```sh
mise run app-tests                # Full suite with race detector
mise run app-quality              # go vet + gofmt check
mise run app-build                # Compile check

# Single package
mise run app-exec go test -race ./internal/preview/domain/

# Single test
mise run app-exec go test -race -run TestDeployPreview_HappyPath ./internal/preview/domain/
```

All three checks (tests, quality, build) must pass before committing.

## Test file conventions

- Tests live in `_test.go` files in the same package (not a `_test` package).
- Mock types are defined at the top of the test file that uses them, not in shared files — with one exception: `internal/agent/domain/test_helpers_test.go` centralizes agent mocks because multiple test files in that package share them.
- Test object factories live in `internal/preview/testharness/factories.go` for building `Preview` structs with sensible defaults.
- Table-driven tests are the norm for exhaustive case coverage (see `TestValidateTransition`, `TestSanitizeForCodeBlock`).

## Mock conventions

All mocks follow the same structure. When adding new mocks, follow these rules exactly.

### Naming

Always `mockComponentName` — lowercase `mock`, PascalCase component name. Examples: `mockRepository`, `mockLLMClient`, `mockPRCommenter`, `mockSlackNotifier`.

### Structure

Every mock has three sections:

```go
type mockSourceDownloader struct {
    mu           sync.Mutex       // 1. Thread safety (always include)
    calls        []downloadCall   // 2. Call recording (for assertions)
    contractYAML string           // 3. Configurable return values (for setup)
    err          error
}
```

**Thread safety**: Always include `sync.Mutex`. Even if the current test doesn't use goroutines, the `-race` detector will catch issues if anything changes later.

**Call recording**: Record every call with full arguments. Use a typed struct for the call record:

```go
type downloadCall struct {
    Owner, Repo, SHA, PAT, DestDir string
}
```

Use slices for all calls (`calls []downloadCall`), and optionally fields for last values when only the final state matters.

**Configurable returns**: Error fields per operation (`err error`, `upErr error`, `generateErr error`). For sequential responses, use a slice with an index counter (see `mockLLMClient.responses`).

### Factory functions

Use `newMockX()` when the mock needs non-trivial initialization:

```go
func newMockRepository() *mockRepository {
    return &mockRepository{previews: make(map[string]Preview)}
}
```

### Accessor methods

For mocks accessed across goroutines, provide thread-safe getters:

```go
func (m *mockRepository) getPreview(owner, repo string, pr int) *Preview {
    m.mu.Lock()
    defer m.mu.Unlock()
    p, ok := m.previews[m.key(owner, repo, pr)]
    if !ok {
        return nil
    }
    return &p
}
```

### Callback hooks

For complex behavior requiring mutation during execution, provide an `onExecute` callback:

```go
type mockToolExecutor struct {
    results   map[string]string
    effects   SideEffects
    onExecute func(ToolCall) // optional: mutate effects on execution
}
```

## HTTP client testing (httptest pattern)

Both the GitHub client and Slack client support URL injection for testing against httptest servers.

### GitHub client

The `Client` struct has a `baseURL` field. All methods use `c.apiURL()` which returns `baseURL` if set, falling back to `"https://api.github.com"`. Tests inject the httptest server URL:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Assert request
    if r.Method != "POST" {
        t.Errorf("Expected POST, got %s", r.Method)
    }
    if r.URL.Path != "/repos/acme/widgets/issues/10/comments" {
        t.Errorf("Unexpected path: %s", r.URL.Path)
    }
    if r.Header.Get("Authorization") != "Bearer ghp_test123" {
        t.Errorf("Missing auth header")
    }

    // Return response
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]interface{}{"id": 999})
}))
defer server.Close()

client := &Client{httpClient: &http.Client{}, baseURL: server.URL}
```

**Critical rule**: Every method on the GitHub client must use `c.apiURL()` to build URLs. Never hardcode `"https://api.github.com"` in a method body. This is what makes httptest interception work.

### Slack client

The Slack client uses `NewClientWithBaseURL(baseURL)` for the same pattern. All methods build URLs from `c.baseURL`.

### Testing tarball downloads

`DownloadSource` downloads and extracts gzip'd tarballs. Tests use a helper that constructs valid tar.gz content in memory:

```go
func createTestTarGz(t *testing.T, rootDir string, files map[string]string) []byte
```

The `rootDir` parameter mimics GitHub's tarball format (`owner-repo-sha/`). The helper creates proper tar entries with that prefix. The extraction code strips the first path component, so test files appear at their relative paths in the dest directory.

### Testing LLM responses

The `LLMClient` interface has a single method: `ChatCompletion(ctx, ChatRequest) (ChatResponse, error)`. The `mockLLMClient` in `agent/domain/test_helpers_test.go` scripts a sequence of responses:

```go
llm := &mockLLMClient{
    responses: []ChatResponse{
        {Content: "I'll create that issue.", FinishReason: "stop"},
    },
}
```

Each call to `ChatCompletion` advances an internal index. If the index exceeds the response slice, it returns a fallback. Parallel error slices allow injecting failures at specific points.

## Preview service integration tests

The preview service (`DeployPreview`) orchestrates 10+ steps across 5 external systems. All are tested through mocks defined in `internal/preview/domain/service_test.go`.

### Test setup

```go
func setupTestService(t *testing.T, opts ...ServiceOption) testDeps
```

Returns the service and all mocks wired together with sensible defaults:
- `mockRepository` — in-memory map, records all calls
- `mockSourceDownloader` — writes contract YAML + placeholder compose file to disk
- `mockComposeManager` — records all generate/up/down/exec calls, writes override files
- `mockHealthChecker` — returns immediately, no polling
- `mockPRCommenter` — records all comment operations
- `mockSlackNotifier` — records all notifications

A standard deploy request:

```go
func testDeployRequest() DeployRequest
// Returns: example-org/my-app PR #42, branch feature/test, full SHA
```

### Why mocks write to the filesystem

Two mocks interact with the real filesystem because the service calls functions that read from disk:

1. **mockSourceDownloader** writes `.productbuilding/preview/config.yml` because `ParseContract()` reads it with `os.ReadFile`. It also writes a placeholder `docker-compose.yml` referenced by the contract.

2. **mockComposeManager.GenerateOverride** writes a minimal override file because the service calls `filepath.Rel(workDir, overridePath)` on the returned path.

Both use the `t.TempDir()` workspace that `setupTestService` creates. This is cleaned up automatically.

### Contract YAML helpers

Three helpers return contract YAML strings for different test scenarios:

- `defaultContractYAML()` — minimal valid contract (app service, port 8080, /healthz)
- `contractWithMigrations()` — adds `database.migrate_command`
- `contractWithPostDeploy()` — adds `post_deploy_commands`

To test a new contract feature, add a similar helper and set `d.dl.contractYAML` before calling `DeployPreview`.

### Injecting failures

Each mock has configurable error fields. Set them before calling `DeployPreview`:

```go
d := setupTestService(t)
d.health.healthyErr = fmt.Errorf("timed out")  // Health check will fail
d.svc.DeployPreview(ctx, req, "ghp_test")
// Assert: preview status is Failed, stage is "healthcheck", TLS check never called
```

### Asserting call sequences

Mocks record calls in slices. Assert on them after `DeployPreview` returns:

```go
d.compose.mu.Lock()
if len(d.compose.upCalls) != 1 {
    t.Fatalf("expected 1 compose up call, got %d", len(d.compose.upCalls))
}
up := d.compose.upCalls[0]
if up.ProjectName != "my-app_pr_42" {
    t.Errorf("wrong project name: %s", up.ProjectName)
}
d.compose.mu.Unlock()
```

Always lock the mock's mutex before reading call slices, even in sequential tests — the race detector enforces this.

### What to test when adding a new deploy step

If you add a new step to `DeployPreview`:

1. Add a mock for any new dependency (following the patterns above).
2. Add a failure test: set the mock's error, assert the preview fails with the correct stage name and that later steps are skipped.
3. Update the happy path test: assert your new mock was called with correct arguments.
4. If the step is conditional (like migrations), add a contract YAML helper and a dedicated test.

## Health checker testing

The health checker supports functional options for test injection:

```go
checker := NewHealthChecker(
    WithPollInterval(1 * time.Millisecond),  // No waiting in tests
    WithHTTPClient(customClient),             // httptest-backed client
    WithTLSClient(customTLSClient),           // httptest-backed TLS client
)
```

In preview service integration tests, the `mockHealthChecker` bypasses the real implementation entirely — it returns immediately with a configurable error. Use the real health checker with options only when testing the polling/timeout logic itself.

## Target registry in tests

The `targets.Registry` struct has an unexported map. For tests, use `Register()` to add targets programmatically:

```go
registry := targets.NewRegistry("productbuilding-")
registry.Register(targets.TargetConfig{
    RepoOwner:     "example-org",
    RepoName:      "my-app",
    GitHubPAT:     "ghp_test",
    SlackChannel:  "#productbuilding-my-app",
    SlackBotToken: "xoxb-test",
})
```

## SQLite repository testing

Repository implementations use injected `*sql.DB`. Tests create in-memory databases:

```go
db, err := database.Connect(":memory:")
// Run migrations
if err := RunTestMigrations(db); err != nil { ... }
// Use the db
repo := NewSQLiteRepository(db)
```

The `RunTestMigrations` function in `slack/infra` loads the embedded migration SQL. Each test gets a fresh database, so there's no cross-test contamination.

## Webhook handler testing

Both GitHub and Slack webhook handlers are tested with full request/response cycles including signature validation.

### GitHub webhooks

```go
// Generate valid signature
sig := generateSignature(payload, "secret123")
req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
req.Header.Set("X-GitHub-Event", "check_run")
req.Header.Set("X-Hub-Signature-256", sig)
rec := httptest.NewRecorder()

handler.HandleWebhook(rec, req)
```

The `generateSignature` helper in `github/web/handlers_test.go` creates valid HMAC-SHA256 signatures. Tests verify that invalid signatures are rejected and that events are routed to the correct handler.

### Slack webhooks

Slack uses a different signature scheme (HMAC-SHA256 with timestamp). The test helpers in `slack/web/handlers_test.go` create valid signatures with current timestamps. Tests also verify replay protection (stale timestamps rejected) and the URL verification challenge-response flow.

## Notifier testing

The notifier uses a two-lane buffer design (see `internal/slack/domain/NOTIFIER.md`). Tests control timing through a `mockDebouncer` that captures callbacks and executes them on demand:

```go
debouncer := newMockDebouncer()
notifier := NewNotifier(client, repo, debouncer, assembler)

notifier.Notify(ctx, event, target)
debouncer.executeAll()  // Trigger the debounced flush immediately

// Assert on what was posted
```

The `retryWait` field on the notifier can be shortened in tests to avoid 5-second sleeps:

```go
notifier.retryWait = 10 * time.Millisecond
```

## Test object factories

`internal/preview/testharness/factories.go` provides builder-pattern constructors:

```go
preview := testharness.NewPreview(
    testharness.WithStatus(domain.StatusReady),
    testharness.WithPR(42),
)
```

Use these in tests outside the domain package. Inside the domain package, construct objects directly since unexported fields are accessible.

## Adding tests for a new vertical

When adding a new vertical (e.g. a new integration), follow this checklist:

1. Define domain interfaces for all external dependencies.
2. Build mock implementations in the test file following the conventions above.
3. Create a `setupTest` helper that wires mocks with sensible defaults.
4. Write a happy-path test that exercises the full flow.
5. Write failure tests for each dependency that can fail.
6. Ensure the httptest pattern is used for any new HTTP clients (never hardcode URLs).
7. Run `mise run app-tests && mise run app-quality && mise run app-build` before committing.
