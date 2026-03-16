# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

Infrastructure and operational tasks (infra-plan, deploy, ssh, etc.) live in deployment repos, not here.

## Architecture

### Core/deployment separation

This repo is the generic core. Each deployment has its own repo (created via `create-deployment`) containing:
- Terraform root module (calls `infrastructure-mgmt/modules/orchestrator/` from this repo via relative path)
- Secrets (age-encrypted AWS creds, GitHub PATs, per-target configs)
- Operational mise tasks (deploy, ssh, infra-plan, instance management)

### Vertical slice structure

The Go app (`orchestrator-app/`) is organized into independent verticals, each with `domain/`, `web/`, `infra/`, and `facade/` sub-packages:

- **agent** — LLM agent loop: prompt assembly -> LLM API call -> tool execution -> response. Multi-provider backend (Anthropic, OpenAI-compatible) with optional fallback. Tools wrap GitHub and Slack actions via adapter pattern.
- **preview** — Preview lifecycle: clone repo, run Docker Compose, track status in SQLite, health-check, report back on PR.
- **github** — Webhook receiver: parses/validates incoming PR/issue events, triggers preview or agent flows.
- **slack** — Slack Events API handler, @mention routing to agent, notification debouncing, thread tracking.
- **dashboard** — Simple web dashboard.
- **platform** — Cross-cutting: config (env vars via `caarlos0/env`), SQLite database + migrations, logging (slog), HTTP server with graceful shutdown, target registry.

### Dependency graph (main.go)

`cmd/server/main.go` constructs the full dependency graph explicitly — no DI framework. Config -> DB -> migrations -> registry -> infra implementations -> domain services -> HTTP handlers -> server.

### Infrastructure

- **OpenTofu**: Reusable module at `infrastructure-mgmt/modules/orchestrator/` (EC2, Traefik, Route53, IAM, per-target resources). Deployment repos call this module with their specific values.
- **Docker Compose**: Production stack has Traefik reverse proxy + orchestrator on shared `preview-net` network. Dev stack mounts source and exposes port 8091. Deployment-specific values (domain, ACME email) are read from environment variables.

### Database

SQLite with migrations in `orchestrator-app/migrations/` (embedded via `embed.FS`). Three tables: `previews` (PR preview state), `slack_threads` (GitHub<->Slack mapping), `agent_conversations` (conversation context).

### Key patterns

- **Adapter pattern**: GitHub/Slack clients wrapped for agent tool consumption (`github_adapter.go`, `slack_adapter.go`).
- **Interface-based testing**: Repositories and external clients are interfaces; tests use mocks.
- **Option functions**: `AgentOption` for optional agent configuration.
- **Reconciliation on startup**: Self-healing after re-provisioning.
