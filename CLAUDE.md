# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A PR preview platform and AI agent orchestration system. It receives GitHub webhooks, deploys PR previews via Docker Compose behind Traefik, integrates bidirectionally with Slack, and runs an LLM-powered agent (Fireworks) that executes GitHub/Slack actions from natural language.

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

To run a single test or package:
```sh
mise run app-exec go test -race -run TestName ./internal/agent/domain/
```

Infrastructure (requires decrypted secrets):
```sh
mise run infra-plan      # OpenTofu plan
mise run infra-apply     # OpenTofu apply
mise run deploy          # SSH deploy: git pull, refresh secrets, rebuild, health check
```

## Architecture

### Vertical slice structure

The Go app (`orchestrator-app/`) is organized into independent verticals, each with `domain/`, `web/`, `infra/`, and `facade/` sub-packages:

- **agent** — LLM agent loop: prompt assembly → Fireworks API call → tool execution → response. Tools wrap GitHub and Slack actions via adapter pattern.
- **preview** — Preview lifecycle: clone repo, run Docker Compose, track status in SQLite, health-check, report back on PR.
- **github** — Webhook receiver: parses/validates incoming PR/issue events, triggers preview or agent flows.
- **slack** — Slack Events API handler, @mention routing to agent, notification debouncing, thread tracking.
- **dashboard** — Simple web dashboard.
- **platform** — Cross-cutting: config (env vars via `caarlos0/env`), SQLite database + migrations, logging (slog), HTTP server with graceful shutdown, target registry.

### Dependency graph (main.go)

`cmd/server/main.go` constructs the full dependency graph explicitly — no DI framework. Config → DB → migrations → registry → infra implementations → domain services → HTTP handlers → server.

### Infrastructure

- **OpenTofu** in `infrastructure-mgmt/`: EC2 + Traefik + Route53 wildcard DNS + per-target-repo resources (Secrets Manager, GitHub webhooks, Actions secrets) via `for_each`.
- **Secrets**: age-encrypted files in `secrets/` (repo-level), AWS Secrets Manager (runtime). Helper at `.mise/lib/load-secrets` loads AWS creds for Terraform.
- **Docker Compose**: Production stack has Traefik reverse proxy + orchestrator on shared `preview-net` network. Dev stack mounts source and exposes port 8091.

### Database

SQLite with migrations in `orchestrator-app/migrations/` (embedded via `embed.FS`). Three tables: `previews` (PR preview state), `slack_threads` (GitHub↔Slack mapping), `agent_conversations` (conversation context).

### Key patterns

- **Adapter pattern**: GitHub/Slack clients wrapped for agent tool consumption (`github_adapter.go`, `slack_adapter.go`).
- **Interface-based testing**: Repositories and external clients are interfaces; tests use mocks.
- **Option functions**: `AgentOption` for optional agent configuration.
- **Reconciliation on startup**: Self-healing after re-provisioning.
