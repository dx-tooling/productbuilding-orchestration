# Luminor ProductBuilding Orchestration

A PR preview platform and AI agent orchestration system. It receives GitHub webhooks, deploys PR previews via Docker Compose behind Traefik, integrates bidirectionally with Slack, and runs an LLM-powered agent that executes GitHub and Slack actions from natural language.

Two main capabilities:

1. **PR previews** — Open a PR on a target repo and a live preview appears at a stable URL (`https://pr-{number}-preview.{domain}`). Updates on new commits, tears down on PR close.
2. **Slack agent** — `@ProductBuilder` in Slack to create issues, request implementations, check status. The agent uses specialist routing to pick the right action (create issue, delegate to coding agent, comment, research, close).

## How it works

```
GitHub (PR/issue events)                         Slack (@mentions)
        │                                                │
        ▼                                                ▼
   ┌─────────────────── Orchestrator (Go) ───────────────────┐
   │                                                         │
   │  github/web ──► preview/domain ──► Docker Compose       │
   │       │              │                   │              │
   │       │              ▼                   ▼              │
   │       │         SQLite state        Traefik routing     │
   │       │              │                                  │
   │       └──► slack/domain ◄── agent/domain ◄── Fireworks  │
   │              │                    │                     │
   └──────────────┼────────────────────┼─────────────────────┘
                  ▼                    ▼
            Slack threads      GitHub issues/PRs
```

The system follows a **bilateral contract**: the orchestrator owns the deployment lifecycle (clone, build, route, health-check, tear down) while each target repo defines its own build and runtime details via `.productbuilding/preview/config.yml`.

Infrastructure: single EC2 instance running Traefik (wildcard TLS via Route53 DNS-01 ACME) + the orchestrator, managed with OpenTofu. Secrets are age-encrypted in the repo and loaded to AWS Secrets Manager at deploy time.

## Quick start

Prerequisites: [mise](https://mise.jdx.dev/) and Docker.

```bash
mise run app-setup    # Build images, start compose, install deps, run tests, start dev server
```

For subsequent sessions:

```bash
mise run app-dev      # Start hot-reload dev server (air) inside the dev container
```

The dev server runs on `localhost:8091`. For production setup and infrastructure provisioning, see [docs/runbook.md](docs/runbook.md).

## Project structure

```
orchestrator-app/
  cmd/server/main.go            Entry point — builds the full dependency graph (no DI framework)
  internal/
    agent/                      LLM agent: prompt assembly → Fireworks API → tool execution
    preview/                    Preview lifecycle: clone, compose, health-check, status tracking
    github/                     Webhook receiver: PR/issue event parsing and validation
    slack/                      Slack Events API: @mentions, notifications, thread tracking
    dashboard/                  Web dashboard for viewing active previews
    platform/                   Cross-cutting: config, SQLite, logging, HTTP server, target registry
  migrations/                   SQLite schema (embedded via embed.FS)

infrastructure-mgmt/
  main/                         OpenTofu: EC2, Traefik, Route53, IAM, per-target resources
  bootstrap/                    One-time S3 + DynamoDB setup for Tofu state

secrets/                        Age-encrypted credentials (see secrets/README.md)
docs/                           Operational documentation
.mise/tasks/                    Mise task implementations
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

### Infrastructure

| Task | What it does |
|------|--------------|
| `mise run infra-plan` | OpenTofu plan (requires decrypted secrets) |
| `mise run infra-apply` | OpenTofu apply |
| `mise run secrets-decrypt` | Decrypt age-encrypted secrets to plaintext |
| `mise run secrets-encrypt` | Re-encrypt plaintext secrets with age |

### Operations

| Task | What it does |
|------|--------------|
| `mise run deploy` | SSH deploy: git pull, refresh secrets, rebuild, health check |
| `mise run ssh` | SSH into orchestrator instance |
| `mise run instance-start` | Start EC2 instance |
| `mise run instance-stop` | Stop EC2 instance (cost savings) |
| `mise run instance-status` | Check EC2 instance state |
| `mise run onboard-target` | Interactive: register new target repo + scaffold files |
| `mise run preview-logs <owner> <repo> <pr> [tail] [follow]` | View preview application logs |

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
    → pending → building → deploying → ready
                                         ↓
                              (new push restarts from pending)

PR closed → deleted (containers removed, resources cleaned up)
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
| `PREVIEW_DOMAIN` | `productbuilder.luminor-tech.net` | Base domain for preview URLs |
| `WORKSPACE_DIR` | `/opt/orchestrator/workspaces` | Directory for cloned repos and compose workspaces |
| `TARGETS_CONFIG_PATH` | `/opt/orchestrator/targets.json` | JSON file with per-target repo configuration |
| `AWS_REGION` | `eu-central-1` | AWS region for Secrets Manager and Route53 |
| `SLACK_SIGNING_SECRET` | — | Slack app signing secret for request verification |
| `SLACK_WORKSPACE` | — | Slack workspace subdomain (e.g. `luminor-tech`) |
| `FIREWORKS_API_KEY` | — | Fireworks API key for LLM calls |
| `FIREWORKS_MODEL` | `accounts/fireworks/models/kimi-k2p5` | Fireworks model identifier |
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
| **Age encryption** | Secrets committed to git (encrypted). Asymmetric: public key in repo, secret key in 1Password. No secrets service needed for local dev. |
| **Vertical slices** | Each feature (agent, preview, github, slack) owns its full stack. New features don't touch other verticals. |
| **Adapter pattern** | GitHub and Slack clients wrapped in adapters for agent tool consumption — agent domain doesn't depend on external API details. |
| **Explicit DI** | `main.go` constructs the full dependency graph. No framework, no magic, easy to trace. |
| **Per-PR mutex** | Prevents concurrent preview operations on the same PR when rapid webhook events arrive. |
| **Reconciliation on startup** | After re-provisioning, the orchestrator rebuilds previews from current GitHub state — self-healing. |
| **Two-lane notification buffer** | Status events overwrite (latest wins), comment events append (all preserved). Eliminates races between rapid state transitions. See `internal/slack/domain/NOTIFIER.md`. |

## Adding a target repo

Quick version:

```bash
mise run onboard-target     # Interactive — registers target + scaffolds files in target repo
mise run secrets-encrypt    # Re-encrypt updated credentials
mise run infra-apply        # Creates webhook, Secrets Manager entry, GitHub Actions secret
mise run deploy             # CRITICAL: restarts orchestrator to load new target config
```

Then in the target repo: install the [OpenCode GitHub App](https://github.com/apps/opencode-agent), customize the scaffolded `.productbuilding/` files, commit and push.

For the full procedure including Slack setup, DNS verification, and all manual steps, see [docs/runbook.md](docs/runbook.md).

## Further reading

- [docs/runbook.md](docs/runbook.md) — Production setup from scratch, ongoing operations
- [CLAUDE.md](CLAUDE.md) — AI assistant context for working with this codebase
- [secrets/README.md](secrets/README.md) — Age encryption scheme and key management
- [orchestrator-app/internal/slack/domain/NOTIFIER.md](orchestrator-app/internal/slack/domain/NOTIFIER.md) — Two-lane notification buffer design
