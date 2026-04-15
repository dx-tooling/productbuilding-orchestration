# AGENTS.md

Project guide for AI coding agents working in this repository.

## What This Is

The generic core of a PR preview platform and AI agent orchestration system. It receives GitHub webhooks, deploys PR previews via Docker Compose behind Traefik, integrates bidirectionally with Slack, and runs an LLM-powered agent that executes GitHub/Slack actions from natural language.

This repo contains the Go application, Docker Compose files, and a reusable Terraform module. Deployment-specific configuration (secrets, infrastructure state, operational tasks) lives in separate deployment repos created with `mise run create-deployment <name>`.

## Commands

All automation goes through **mise** tasks. The Go app runs inside a Docker dev container — `app-exec` wraps commands to run inside it.

| Task | What it does |
|---|---|
| `mise run app-tests` | Unit tests with `-race` (`go test -race ./internal/...`) |
| `mise run app-build` | Compile Go binary to `orchestrator-app/bin/server` |
| `mise run app-quality` | `go vet` + `gofmt` check |
| `mise run app-dev` | Start hot-reload dev server (air) inside the dev container |
| `mise run app-setup` | Full bootstrap: build images, start compose, deps, tests, dev server |
| `mise run app-compose -- <args>` | Proxy to `docker compose` using `docker-compose.dev.yml` |
| `mise run app-exec <cmd>` | Run a command inside the app container (e.g. `mise run app-exec go test ./internal/agent/...`) |
| `mise run create-deployment <name>` | Scaffold a new deployment repo at `../productbuilding-deployment-<name>/` |

To run a single test or package:
```sh
mise run app-exec go test -race -run TestName ./internal/agent/domain/
```

Infrastructure and operational tasks (infra-plan, deploy, ssh, switch-llm-provider, etc.) live in deployment repos, not here.

## Architecture

### Core/deployment separation

This repo is the generic core. Each deployment has its own repo (created via `create-deployment`) containing:
- Terraform root module (calls `infrastructure-mgmt/modules/orchestrator/` from this repo via relative path)
- Secrets (age-encrypted AWS creds, GitHub PATs, per-target configs)
- Operational mise tasks (deploy, ssh, infra-plan, instance management)

### Vertical slice structure

The Go app (`orchestrator-app/`) is organized into independent verticals, each with `domain/`, `web/`, `infra/`, and `facade/` sub-packages:

- **agent** — LLM agent loop: router classifies intent → specialist executes (issue_creator, delegator, commenter, researcher, closer, event_narrator). Multi-provider backend (Anthropic, OpenAI-compatible) with optional fallback. Tools wrap GitHub and Slack actions via adapter pattern. Prompts are in `specialist_prompts.go`. **Scope boundary**: the system's job ends at producing good PRs — it does NOT merge. When users approve, the delegator posts an approval summary; merging is a developer responsibility.
- **preview** — Preview lifecycle: clone repo, run Docker Compose, track status in SQLite, health-check, report back on PR.
- **github** — Webhook receiver: parses/validates incoming PR/issue events, triggers preview or agent flows.
- **slack** — Slack Events API handler, @mention routing to agent, notification debouncing, thread tracking. See `internal/slack/domain/NOTIFIER.md` for the two-lane event buffer design.
- **dashboard** — Simple web dashboard.
- **platform** — Cross-cutting: config (env vars via `caarlos0/env`), SQLite database + migrations, logging (slog), HTTP server with graceful shutdown, target registry.
- **featurecontext** — Cross-cutting context assembly: gathers issue state, PR details, CI check runs, and preview status into a single `FeatureSnapshot`. Used by the Slack notifier (to enrich notifications) and the Slack handler (to inject feature state into agent context). Adapters in `adapters.go` bridge the GitHub client and preview repository to consumer-side interfaces.

### Dependency graph (main.go)

`cmd/server/main.go` constructs the full dependency graph explicitly — no DI framework. Config -> DB -> migrations -> registry -> feature assembler -> infra implementations -> domain services -> HTTP handlers -> server.

### Infrastructure

- **OpenTofu**: Reusable module at `infrastructure-mgmt/modules/orchestrator/` (EC2, Traefik, Route53, IAM, per-target resources). Deployment repos call this module with their specific values.
- **Docker Compose**: Production stack has Traefik reverse proxy + orchestrator on shared `preview-net` network. Dev stack mounts source and exposes port 8091. Deployment-specific values (domain, ACME email) are read from environment variables.

### Database

SQLite with migrations in `orchestrator-app/migrations/` (embedded via `embed.FS`). Three tables: `previews` (PR preview state), `slack_threads` (GitHub<->Slack mapping), `agent_conversations` (conversation context).

### Key patterns

- **Adapter pattern**: GitHub/Slack clients wrapped for agent tool consumption (`github_adapter.go`, `slack_adapter.go`). The `featurecontext` package uses the same pattern — `GitHubIssueAdapter`, `GitHubPRAdapter`, etc. bridge the GitHub client to consumer-defined interfaces.
- **Interface-based testing**: Every external dependency is behind a domain interface. Tests inject mock implementations — no real Docker, GitHub API, Slack API, or network calls needed. See `TESTING.md` for patterns and conventions.
- **Functional options**: `ServiceOption` on preview service (e.g. `WithSlackThreadChecker`), `HealthCheckerOption` on health checker (e.g. `WithPollInterval`, `WithHTTPClient`), `AgentOption` on agent configuration. Follow this pattern when adding optional dependencies.
- **baseURL injection**: Both the GitHub client and Slack client have a `baseURL` field that defaults to the production API URL but can be overridden for httptest servers in tests. All HTTP methods must use `c.apiURL()` (GitHub) or `c.baseURL` (Slack) — never hardcode API URLs.
- **Reconciliation on startup**: Self-healing after re-provisioning.

## Testing

Every external system integration is fully testable through deterministic mocks. No test in this codebase requires Docker, network access, or real API calls.

See `TESTING.md` for the complete guide: mock conventions, httptest patterns, how to write integration tests for the preview service, and how to add tests for new features.

### Quality gate

All three checks must pass before committing:

```sh
mise run app-tests    # Unit tests with race detector
mise run app-quality  # go vet + gofmt
mise run app-build    # Compile check
```
