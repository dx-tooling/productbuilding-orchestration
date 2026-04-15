# ProductBuilding Orchestration

A PR preview platform and AI agent orchestration system. It receives GitHub webhooks, deploys PR previews via Docker Compose behind Traefik, integrates bidirectionally with Slack, and runs an LLM-powered agent that executes GitHub and Slack actions from natural language.

Two main capabilities:

1. **PR previews** — Open a PR on a target repo and a live preview appears at a stable URL (`https://pr-{number}-preview.{domain}`). Updates on new commits, tears down on PR close.
2. **Slack agent** — `@ProductBuilder` in Slack to create issues, request implementations, check status. The agent uses specialist routing to pick the right action (create issue, delegate to coding agent, comment, research, close). The system's scope ends at producing good pull requests — merging is a developer responsibility.

## Core + Deployment separation

This repository is the **generic core** — it contains the Go application, Docker Compose files, and a reusable Terraform module. It does NOT contain secrets, infrastructure state, or deployment-specific configuration.

Each deployment (a specific AWS account + domain + GitHub org) lives in its own **deployment repo** created with:

```bash
mise run create-deployment <name>
# Creates ../productbuilding-deployment-<name>/ with infrastructure, secrets, and operational tasks
```

The deployment repo sits alongside this one and references the Terraform module via relative path. For app development, work in this repo. For infrastructure and operations, work from the deployment repo.

## How it works

```
GitHub (PR/issue events)                         Slack (@mentions)
        |                                                |
        v                                                v
   +------------------- Orchestrator (Go) -------------------+
   |                                                         |
   |  github/web --> preview/domain --> Docker Compose       |
   |       |              |                   |              |
   |       |              v                   v              |
   |       |         SQLite state        Traefik routing     |
   |       |              |                                  |
   |       +---> slack/domain <-- agent/domain <-- LLM API   |
   |              |                    |                     |
   +--------------------------------------------+-----------+
                  v                    v
            Slack threads      GitHub issues/PRs
```

The system follows a **bilateral contract**: the orchestrator owns the deployment lifecycle (clone, build, route, health-check, tear down) while each target repo defines its own build and runtime details via `.productbuilding/preview/config.yml`.

Infrastructure: single EC2 instance running Traefik (wildcard TLS via Route53 DNS-01 ACME) + the orchestrator, managed with OpenTofu. The Terraform module is at `infrastructure-mgmt/modules/orchestrator/`.

## Quick start

Prerequisites: [mise](https://mise.jdx.dev/) and Docker.

```bash
mise run app-setup    # Build images, start compose, install deps, run tests, start dev server
```

For subsequent sessions:

```bash
mise run app-dev      # Start hot-reload dev server (air) inside the dev container
```

The dev server runs on `localhost:8091`.

## Project structure

```
orchestrator-app/
  cmd/server/main.go            Entry point — builds the full dependency graph (no DI framework)
  internal/
    agent/                      LLM agent: prompt assembly -> LLM API -> tool execution
    preview/                    Preview lifecycle: clone, compose, health-check, status tracking
    github/                     Webhook receiver: PR/issue event parsing and validation
    slack/                      Slack Events API: @mentions, notifications, thread tracking
    dashboard/                  Web dashboard for viewing active previews
    platform/                   Cross-cutting: config, SQLite, logging, HTTP server, target registry
  migrations/                   SQLite schema (embedded via embed.FS)

infrastructure-mgmt/
  modules/orchestrator/         Reusable Terraform module: EC2, Traefik, Route53, IAM, per-target resources

.mise/tasks/                    Development tasks (app-build, app-tests, etc.)
```

### Vertical slices

Each vertical under `internal/` is organized into sub-packages:

| Layer | Purpose |
|-------|---------|
| `domain/` | Business logic, models, interfaces |
| `web/` | HTTP handlers and route registration |
| `infra/` | Interface implementations (SQLite repos, external clients) |
| `facade/` | DTOs for cross-vertical communication |

Verticals communicate through interfaces defined in `domain/`. The dependency graph is wired explicitly in `main.go`.

One cross-cutting package sits outside the vertical structure: `internal/featurecontext/` aggregates issue, PR, CI, and preview state into a single `FeatureSnapshot` used by both the Slack notifier (to enrich notifications) and the agent handler (to provide pre-loaded context).

### Agent architecture

The LLM agent uses a **router → specialist** pattern. A router LLM call classifies user intent, then dispatches to one or more focused specialists:

| Specialist | Role |
|------------|------|
| `issue_creator` | Creates GitHub issues (searches for duplicates first) |
| `delegator` | Delegates work to OpenCode by posting `/opencode` comments on issues or PRs |
| `commenter` | Posts plain comments on GitHub issues |
| `researcher` | Answers questions by searching issues, code, PR diffs, CI status, conversation history |
| `closer` | Closes GitHub issues or pull requests |
| `event_narrator` | Translates automated system events into natural-language Slack updates |

**Scope boundary**: The system helps non-technical users go from idea to pull request. It does NOT merge PRs — merging is a developer responsibility. When a user approves ("ship it"), the bot posts an approval summary on the PR and hands off.

**Workstream phases** track where each conversation is in its lifecycle: `intake` → `open` → `in-progress` → `review` → `revision` → `done`. The router uses the current phase to disambiguate intent (e.g., "looks good" means different things during intake vs. after a preview).

**PR-centric delegation**: Once a PR exists for a workstream, the delegator posts `/opencode` comments on the PR (not the issue), so OpenCode naturally works on the PR's branch.

### Database

SQLite with three tables:

| Table | Purpose |
|-------|---------|
| `previews` | PR preview state: repo, PR number, status, SHA, preview URL, compose project |
| `slack_threads` | GitHub-to-Slack mapping: issue/PR number to channel + thread timestamp |
| `agent_conversations` | LLM conversation context: channel, thread, summary, linked issue |

## Mise tasks

### Development

| Task | What it does |
|------|--------------|
| `mise run app-setup` | Full bootstrap: build images, start compose, deps, tests, dev server |
| `mise run app-dev` | Start hot-reload dev server (air) inside the dev container |
| `mise run app-build` | Compile Go binary to `orchestrator-app/bin/server` |
| `mise run app-quality` | `go vet` + `gofmt` check |
| `mise run app-tests` | Unit tests with `-race` (`go test -race ./internal/...`) |
| `mise run app-exec <cmd>` | Run a command inside the app container |
| `mise run app-compose -- <args>` | Proxy to `docker compose` using `docker-compose.dev.yml` |

To run a single test:
```bash
mise run app-exec go test -race -run TestName ./internal/agent/domain/
```

### Deployment scaffolding

| Task | What it does |
|------|--------------|
| `mise run create-deployment <name>` | Scaffold a new deployment repo at `../productbuilding-deployment-<name>/` |

Infrastructure, secrets, and operational tasks (deploy, ssh, infra-plan, etc.) live in the deployment repo.

## Preview contract

Target repos define their preview deployment in `.productbuilding/preview/config.yml`. The orchestrator reads this file after cloning the repo and uses it to generate Docker Compose overrides with Traefik labels.

### Full reference

```yaml
# Required
version: 1

compose:
  file: .productbuilding/preview/docker-compose.yml   # Path to Compose file (relative to repo root)
  service: app                                         # Main service name in the Compose file

runtime:
  internal_port: 8080              # Port the app listens on inside the container
  healthcheck_path: /healthz       # Path for health check polling
  startup_timeout_seconds: 300     # Max seconds to wait for healthy response

# Optional
database:
  migrate_command: go run ./cmd/migrate up   # Run inside the main service container after start

logging:
  service: app          # Compose service to get logs from (defaults to compose.service)
  type: docker           # "docker" (default, uses compose logs) or "file" (tails container files)
  path: /var/log/app.log # Required when type=file; supports globs like /var/log/app/*.log

user_facing_note: "Test login: admin / secret"   # Shown in the PR comment

post_deploy_commands:                    # Run after preview is healthy
  - service: app                         # Compose service (defaults to compose.service)
    command: /app/seed-data              # Command to execute
    description: Seed demo data          # Human-readable label for logs
```

### Preview lifecycle

```
PR opened/reopened/synchronize
    -> pending -> building -> deploying -> ready
                                         |
                              (new push restarts from pending)

PR closed -> deleted (containers removed, resources cleaned up)
```

On failure at any stage, the preview moves to `failed` with the error stage and message recorded. The PR comment is updated at each state transition.

## HTTP API

| Method | Path | Vertical | Purpose |
|--------|------|----------|---------|
| `GET` | `/` | dashboard | Web dashboard showing active previews |
| `GET` | `/previews` | preview | List all previews (JSON) |
| `GET` | `/previews/{owner}/{repo}/{pr}/logs` | preview | Stream preview logs (`?tail=100&follow=false`) |
| `POST` | `/webhook` | github | GitHub webhook receiver (HMAC-SHA256 validated) |
| `POST` | `/slack/events` | slack | Slack Events API (signing secret validated) |
| `GET` | `/healthz` | platform | Health check (always 200) |
| `GET` | `/readyz` | platform | Readiness check (pings SQLite) |

## Configuration

All configuration is via environment variables, loaded with [caarlos0/env](https://github.com/caarlos0/env).

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_ENV` | `development` | `development` or `production` (controls log format) |
| `PORT` | `8080` | HTTP server listen port |
| `DATABASE_PATH` | `data/orchestrator.db` | SQLite database file path |
| `PREVIEW_DOMAIN` | — | Base domain for preview URLs |
| `WORKSPACE_DIR` | `/opt/orchestrator/workspaces` | Directory for cloned repos and compose workspaces |
| `TARGETS_CONFIG_PATH` | `/opt/orchestrator/targets.json` | JSON file with per-target repo configuration |
| `AWS_REGION` | `eu-central-1` | AWS region for Secrets Manager and Route53 |
| `SLACK_SIGNING_SECRET` | — | Slack app signing secret for request verification |
| `SLACK_WORKSPACE` | — | Slack workspace subdomain |
| `LLM_PROVIDER` | `anthropic` | LLM provider: `anthropic` or `openaicompat` |
| `LLM_API_KEY` | — | API key for the primary LLM provider |
| `LLM_MODEL` | `claude-opus-4-6` | Model identifier for the primary provider |
| `LLM_BASE_URL` | — | Base URL for `openaicompat` provider (e.g. `https://api.fireworks.ai/inference/v1`) |
| `LLM_FALLBACK_PROVIDER` | — | Optional fallback provider type |
| `LLM_FALLBACK_API_KEY` | — | API key for the fallback provider |
| `LLM_FALLBACK_MODEL` | — | Model for the fallback provider |
| `LLM_FALLBACK_BASE_URL` | — | Base URL for the fallback provider (if `openaicompat`) |
| `SLACK_CHANNEL_PREFIX` | `productbuilding-` | Prefix for Slack channel-to-repo matching |
| `ACME_EMAIL` | `admin@example.com` | Email for Let's Encrypt ACME certificates |
| `LLM_REQUEST_TIMEOUT_SECS` | `60` | Timeout per LLM API request |
| `LLM_MAX_RETRIES` | `3` | Max retries for failed LLM requests |
| `AGENT_RUN_TIMEOUT_SECS` | `120` | Max total time for an agent run |
| `AGENT_TOKEN_BUDGET` | `8000` | Token budget for agent context assembly |

## Architecture decisions

| Decision | Rationale |
|----------|-----------|
| **SQLite** | Single instance, no external database dependency. Embedded migrations via `embed.FS`. |
| **Tarball API** | Download source via GitHub API instead of git clone — avoids needing git on the server, simpler and faster. |
| **Docker socket mount** | Orchestrator controls Docker Compose directly through the socket — no intermediary. |
| **Core/deployment split** | Generic capabilities in this repo, deployment-specific config in separate private repos. Enables multiple independent deployments from one codebase. |
| **Vertical slices** | Each feature (agent, preview, github, slack) owns its full stack. New features don't touch other verticals. |
| **Adapter pattern** | GitHub and Slack clients wrapped in adapters for agent tool consumption — agent domain doesn't depend on external API details. |
| **Explicit DI** | `main.go` constructs the full dependency graph. No framework, no magic, easy to trace. |
| **Per-PR mutex** | Prevents concurrent preview operations on the same PR when rapid webhook events arrive. |
| **Reconciliation on startup** | After re-provisioning, the orchestrator rebuilds previews from current GitHub state — self-healing. |
| **Two-lane notification buffer** | Status events overwrite (latest wins), comment events append (all preserved). Eliminates races between rapid state transitions. See `internal/slack/domain/NOTIFIER.md`. |

## Further reading

- [AGENTS.md](AGENTS.md) — Project guide for AI coding agents working in this codebase
- [TESTING.md](TESTING.md) — Testing guide: mock conventions, httptest patterns, preview service integration tests
- [orchestrator-app/internal/slack/domain/NOTIFIER.md](orchestrator-app/internal/slack/domain/NOTIFIER.md) — Two-lane notification buffer design, feature context assembly, message generation
