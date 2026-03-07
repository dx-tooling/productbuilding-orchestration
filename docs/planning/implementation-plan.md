# Preview Orchestrator — Implementation Plan

## 1. Architecture Overview

### System Topology

A single EC2 instance runs everything:

```
EC2 Instance (e.g. t3.xlarge)
|
+-- Docker Engine
|   |
|   +-- [orchestrator stack] docker-compose.yml
|   |   +-- traefik          (reverse proxy, TLS, routing)
|   |   +-- orchestrator     (Go app, webhook receiver, preview manager)
|   |
|   +-- [preview stack] etfg-app-starter-kit_pr_42/docker-compose.yml
|   |   +-- app
|   |   +-- db-business
|   |   +-- db-rag
|   |   +-- local-inference
|   |
|   +-- [preview stack] other-repo_pr_7/docker-compose.yml
|   |   +-- ...
|   |
|   +-- [shared] preview-net  (external Docker network)
|
+-- /var/run/docker.sock  (mounted into traefik + orchestrator)
```

### Network Flow

```
Internet
  -> Route53 wildcard DNS (*.productbuilder.luminor-tech.net -> Elastic IP)
    -> Traefik (ports 80/443, wildcard Let's Encrypt cert via DNS-01 + Route53)
      -> api.productbuilder.luminor-tech.net                        -> orchestrator container
      -> etfg-app-starter-kit-pr-42.productbuilder.luminor-tech.net -> etfg-app-starter-kit_pr_42 app
      -> other-repo-pr-7.productbuilder.luminor-tech.net            -> other-repo_pr_7 app
```

### Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Trigger model | GitHub webhook | Self-contained; no per-repo workflow files |
| State store | SQLite | Single-process app, state is reconstructable |
| Code fetch | GitHub tarball API | Fast, no git dependency in container |
| Build strategy | Build on server | Fewer moving parts for MVP |
| GitHub feedback | PR comment (updated in place) | Visible, simple, no deployment API complexity |
| Docker access | Socket mount | Single-purpose server, admin party |
| IaC | OpenTofu | Declarative, S3 backend, GitHub provider for webhooks |
| Secrets | AWS Secrets Manager | Automated provisioning, no manual SCP |
| TLS | Traefik ACME + Route53 DNS-01 | Wildcard cert, fully automated |
| Startup resilience | Reconciliation loop | Self-healing after re-provisioning |

---

## 2. Infrastructure as Code

### 2.1 Directory Layout

```
infrastructure-mgmt/
  bootstrap/              # One-time setup, local state
    main.tf               # S3 bucket, DynamoDB table
    variables.tf
    outputs.tf
    terraform.tfstate     # Local (committed to .gitignore)
  main/                   # Ongoing management, S3 backend
    backend.tf            # S3 + DynamoDB backend config
    main.tf               # EC2, EIP, SG, Route53, IAM
    targets.tf            # Per-target-repo resources (webhooks, secrets, GH Actions secrets)
    variables.tf
    outputs.tf
    cloud-init.yml        # Instance bootstrap script (templatefile)
    providers.tf          # AWS + GitHub providers
    targets.auto.tfvars   # Gitignored — target repo credentials
  onboard-target.sh       # Interactive script to add a new target repo
```

### 2.2 Bootstrap Project (`infrastructure-mgmt/bootstrap/`)

Creates the prerequisites for the main project. Run once manually with local state.

**Resources:**

- S3 bucket for OpenTofu state (versioning enabled)
- DynamoDB table for state locking
- AWS Secrets Manager secrets (one per target repo, created by main project — bootstrap only creates the bucket/table)

**Recipe:**

1. `cd infrastructure-mgmt/bootstrap`
2. Fill in AWS credentials (from `secrets.yaml`)
3. `tofu init && tofu apply`
4. Commit `outputs.tf` values, never commit `terraform.tfstate`

### 2.3 Main Project (`infrastructure-mgmt/main/`)

Uses S3 backend. Manages all ongoing infrastructure.

**Resources:**

- **Route53 hosted zone**: `productbuilder.luminor-tech.net`
- **Route53 wildcard record**: `*.productbuilder.luminor-tech.net` -> Elastic IP
- **Elastic IP**: stable public IP for the instance
- **Security group**: inbound 80, 443, 22; outbound all
- **IAM instance profile + role**: permissions for Secrets Manager read, Route53 record management (Traefik DNS-01), ECR pull (future)
- **EC2 instance**: Amazon Linux 2023 or Ubuntu 24.04, variable instance type (default `t3.xlarge`)
- **Per-target-repo resources** (via `for_each` over targets):
  - Secrets Manager secret: `productbuilder/targets/{repo_name}` with keys `github_pat`, `webhook_secret`
  - GitHub webhook: PR events, pointing to `https://api.productbuilder.luminor-tech.net/webhook`
  - GitHub Actions secret: `ANTHROPIC_API_KEY` on the target repo (for Claude Code)

**Target registry** (`infrastructure-mgmt/main/targets.auto.tfvars`, gitignored):

```hcl
targets = {
  "etfg-app-starter-kit" = {
    repo_owner         = "luminor-tech"
    repo_name          = "etfg-app-starter-kit"
    github_pat         = "github_pat_..."
    webhook_secret     = "a1b2c3..."          # openssl rand -hex 32
    anthropic_api_key  = "sk-ant-..."          # for Claude Code GitHub Actions
  }
  # Add future target repos here
}
```

OpenTofu iterates over this map to create webhooks, Secrets Manager entries, and GitHub Actions secrets per target. Onboarding a new repo = run `onboard-target.sh` + `tofu apply`.

### 2.4 Target Repo Onboarding Script

`infrastructure-mgmt/onboard-target.sh` automates the mechanical parts of adding a new target repo:

```
$ ./onboard-target.sh

Target repo onboarding
======================
Repo owner [luminor-tech]: luminor-tech
Repo name: my-new-app
GitHub PAT (fine-grained, scoped to this repo): github_pat_...
Anthropic API key (for Claude Code): sk-ant-...
Generating webhook secret... done (e4f8a1...)

Appending to main/targets.auto.tfvars... done.

Next steps:
  1. cd main && tofu apply
  2. Install the Claude GitHub App on the repo:
     https://github.com/apps/claude
  3. Add .github/workflows/claude.yml to the repo (see docs)
  4. Add preview.yml + docker-compose.preview.yml to the repo (see docs)
```

The script:

1. Prompts for repo owner, repo name, fine-grained PAT, Anthropic API key
2. Generates a webhook secret via `openssl rand -hex 32`
3. Appends the new target entry to `main/targets.auto.tfvars`
4. Prints remaining manual steps (Claude App install, workflow file, preview contract)

The **one step that cannot be automated** is installing the Claude GitHub App — it requires an OAuth consent flow in the browser. The script prints the direct link.

**Cloud-init script** (templated by OpenTofu):

1. Install Docker Engine + Docker Compose plugin
2. Create `preview-net` external Docker network
3. Fetch target secrets from AWS Secrets Manager
4. Write orchestrator config (`/opt/orchestrator/config.yaml`) including target registry
5. Pull orchestrator Docker image (or clone repo + build)
6. `docker compose up -d` the orchestrator stack
7. Signal completion (optional: CloudWatch or simple health check)

**Manual step (one-time):**

At the apex domain hoster, create NS records for `productbuilder.luminor-tech.net` pointing to the four Route53 nameservers output by `tofu apply`.

### 2.5 Re-provisioning Flow

"Cattle" workflow — destroy and recreate at will:

1. `cd infrastructure-mgmt/main && tofu apply` (creates fresh EC2 instance, re-associates EIP)
2. Cloud-init bootstraps everything automatically
3. Orchestrator starts, runs reconciliation loop
4. Reconciliation queries GitHub for open PRs, rebuilds all previews
5. Updates stale PR comments

No manual intervention needed after the one-time NS delegation and secret population.

---

## 3. Orchestrator Application

### 3.1 Project Structure

```
cmd/
  server/
    main.go               # Entry point, DI wiring
  cli/
    main.go               # CLI commands (reconcile, status, etc.)

internal/
  preview/                # Core domain vertical
    domain/
      preview.go          # Preview entity, state machine
      service.go          # Preview lifecycle logic
      repository.go       # Repository interface
      service_test.go
    facade/
      dto.go              # DTOs for cross-vertical use
      events.go           # PreviewCreated, PreviewReady, PreviewFailed, etc.
    infra/
      sqlite_repository.go
    web/
      handlers.go         # GET /previews, GET /previews/{id}
      routes.go
    testharness/
      factories.go

  github/                 # GitHub integration vertical
    domain/
      webhook.go          # Webhook payload parsing + signature validation
      comment.go          # PR comment management logic
      tarball.go          # Repo tarball download logic
      service.go
      service_test.go
    facade/
      dto.go              # PREvent, CommentUpdate, etc.
    infra/
      github_client.go    # HTTP client for GitHub API
    web/
      handlers.go         # POST /webhook
      routes.go
    testharness/
      factories.go

  orchestration/          # Docker/Compose management vertical
    domain/
      deployer.go         # Compose project lifecycle
      network.go          # Shared network management
      healthcheck.go      # Container health polling
      labels.go           # Traefik label generation
      service.go
      service_test.go
    facade/
      dto.go              # DeploymentResult, ContainerStatus, etc.
    infra/
      docker_client.go    # Docker Engine API client
      compose.go          # Shell out to docker compose
    testharness/
      factories.go

  dashboard/              # Web UI vertical
    web/
      handlers.go         # GET / (dashboard page)
      routes.go
      templates/          # templ templates

  platform/               # Shared infrastructure
    config/               # App configuration (env-based)
    database/             # SQLite connection, migrations
    http/                 # Server setup, middleware
    logging/              # Structured logging (slog)

migrations/
  001_create_previews.up.sql
  001_create_previews.down.sql

docker/
  prod/
    Dockerfile            # Multi-stage: build + distroless runtime

docker-compose.yml        # Orchestrator stack (orchestrator + traefik)
docker-compose.dev.yml    # Development overrides

mise.toml                 # Task runner
.golangci.yml             # Linter config
.air.toml                 # Hot reload (dev)

docs/
  archbook.md
  techbook.md
  devbook.md
  runbook.md
  planning/
    initial-design-brief.md
    implementation-plan.md
```

### 3.2 Preview Entity and State Machine

```
              opened / reopened
                    |
                    v
              [ pending ]
                    |
                    v
              [ building ]  -- fetch tarball, docker compose build
                    |
                    v
              [ deploying ] -- docker compose up, attach to preview-net
                    |
               +---------+
               |         |
               v         v
          [ ready ]  [ failed ]
               |         |
               |    (new push)
               |         |
          (new push)     |
               |         |
               v         v
          [ pending ] ----+  (cycle back)

          (PR closed/merged)
               |
               v
          [ deleted ]  -- docker compose down, remove resources
```

**States:**

| State | Meaning |
|-------|---------|
| `pending` | Event received, queued for processing |
| `building` | Tarball downloaded, `docker compose build` running |
| `deploying` | `docker compose up` running, waiting for health check |
| `ready` | Health check passed, preview is live |
| `failed` | Any stage failed, error recorded |
| `deleted` | PR closed, resources torn down |

### 3.3 Target Registry

On startup, the orchestrator loads target repo configurations from AWS Secrets Manager. Each target has a secret at `productbuilder/targets/{repo_name}` containing:

```json
{
  "repo_owner": "luminor-tech",
  "repo_name": "etfg-app-starter-kit",
  "github_pat": "github_pat_...",
  "webhook_secret": "a1b2c3..."
}
```

The orchestrator holds these in memory as a map keyed by `{repo_owner}/{repo_name}`. When a webhook arrives, it identifies the target repo from the payload, looks up the corresponding PAT and webhook secret, and uses them for all GitHub API calls and signature validation for that repo.

### 3.4 Data Model

**Table: `previews`**

```sql
CREATE TABLE previews (
    id                  TEXT PRIMARY KEY,
    repo_owner          TEXT NOT NULL,
    repo_name           TEXT NOT NULL,
    pr_number           INTEGER NOT NULL,
    branch_name         TEXT NOT NULL,
    head_sha            TEXT NOT NULL,
    preview_url         TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    compose_project     TEXT NOT NULL,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_successful_sha TEXT,
    error_stage         TEXT,
    error_message       TEXT,
    github_comment_id   INTEGER,
    UNIQUE(repo_owner, repo_name, pr_number)
);
```

### 3.5 Core Workflows

#### Webhook Received (PR opened / synchronize / reopened)

1. Validate webhook signature (HMAC-SHA256, using the target repo's webhook secret)
2. Parse PR event payload, extract: repo owner/name, PR number, branch, head SHA, action
3. Look up target repo config (PAT, webhook secret) from in-memory target registry
4. Upsert preview record → status `pending`
5. Post/update PR comment: "Preview building..." (using target repo's PAT)
6. Download tarball from GitHub API at head SHA (using target repo's PAT)
7. Extract to `/opt/orchestrator/workspaces/{repo_name}/preview_pr_{number}/`
8. Read `preview.yml` contract from extracted source
9. Update status → `building`
10. Generate `docker-compose.override.yml` with:
    - Traefik labels for routing (`{repo_name}-pr-{number}.productbuilder.luminor-tech.net`)
    - Network attachment to `preview-net`
    - Resource limits (CPU, memory)
    - Environment variables from preview contract
11. Run `docker compose -p {repo_name}_pr_{number} build`
12. Update status → `deploying`
13. Run `docker compose -p {repo_name}_pr_{number} up -d`
14. Poll health check endpoint (from preview contract) with timeout
15. On success:
    - Update status → `ready`, set `last_successful_sha`
    - Update PR comment: "Preview ready: https://{repo_name}-pr-{number}.productbuilder.luminor-tech.net"
16. On failure:
    - Update status → `failed`, record `error_stage` + `error_message`
    - Update PR comment: "Preview failed: {concise reason}"

#### Webhook Received (PR closed)

1. Validate webhook signature
2. Run `docker compose -p {repo_name}_pr_{number} down -v`
3. Remove workspace directory
4. Update preview record → status `deleted`
5. Update PR comment: "Preview removed."

#### Reconciliation (on startup, optionally periodic)

1. Query GitHub API: list open PRs for each registered target repo
2. Query local DB: list all previews not in `deleted` status
3. For each open PR without a preview: trigger creation
4. For each open PR with an outdated `head_sha`: trigger update
5. For each preview whose PR is no longer open: trigger deletion
6. Reconcile Docker state: check for orphaned Compose projects not in DB → remove them

### 3.6 Orchestrator Docker Compose Stack

```yaml
# docker-compose.yml (orchestrator stack)

services:
  traefik:
    image: traefik:v3
    command:
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.network=preview-net"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--entrypoints.web.http.redirections.entrypoint.to=websecure"
      - "--certificatesresolvers.le.acme.dnschallenge=true"
      - "--certificatesresolvers.le.acme.dnschallenge.provider=route53"
      - "--certificatesresolvers.le.acme.email=admin@luminor-tech.net"
      - "--certificatesresolvers.le.acme.storage=/letsencrypt/acme.json"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - letsencrypt:/letsencrypt
    networks:
      - preview-net
    environment:
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AWS_REGION=${AWS_REGION}
    restart: unless-stopped

  orchestrator:
    build:
      context: .
      dockerfile: docker/prod/Dockerfile
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - orchestrator-data:/data          # SQLite DB
      - workspaces:/opt/orchestrator/workspaces  # Extracted tarballs
    networks:
      - preview-net
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.orchestrator.rule=Host(`api.productbuilder.luminor-tech.net`)"
      - "traefik.http.routers.orchestrator.entrypoints=websecure"
      - "traefik.http.routers.orchestrator.tls.certresolver=le"
      - "traefik.http.services.orchestrator.loadbalancer.server.port=8080"
    environment:
      - DATABASE_PATH=/data/orchestrator.db
      - TARGETS_CONFIG_PATH=/data/targets.json
      - PREVIEW_DOMAIN=productbuilder.luminor-tech.net
      - WORKSPACE_DIR=/opt/orchestrator/workspaces
      - AWS_REGION=${AWS_REGION}
    restart: unless-stopped

networks:
  preview-net:
    external: true

volumes:
  letsencrypt:
  orchestrator-data:
  workspaces:
```

### 3.7 Generated Preview Compose Override

The orchestrator generates a `docker-compose.override.yml` for each preview, adding Traefik integration and resource limits:

```yaml
# Generated for etfg-app-starter-kit PR 42, placed alongside the repo's docker-compose.preview.yml

services:
  app:
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.etfg-app-starter-kit-pr-42.rule=Host(`etfg-app-starter-kit-pr-42.productbuilder.luminor-tech.net`)"
      - "traefik.http.routers.etfg-app-starter-kit-pr-42.entrypoints=websecure"
      - "traefik.http.routers.etfg-app-starter-kit-pr-42.tls.certresolver=le"
      - "traefik.http.services.etfg-app-starter-kit-pr-42.loadbalancer.server.port=3000"
    networks:
      - preview-net
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: "1.0"

networks:
  preview-net:
    external: true
```

### 3.8 Preview Contract (in target repo)

Example `preview.yml` for etfg-app-starter-kit:

```yaml
version: 1

compose:
  file: docker-compose.preview.yml
  service: app

runtime:
  internal_port: 8080
  healthcheck_path: /healthz
  startup_timeout_seconds: 300

database:
  migrate_command: go run ./cmd/migrate business up && go run ./cmd/migrate rag up
```

The target repo also provides `docker-compose.preview.yml` — a production-oriented Compose file (using the prod Dockerfile, no hot reload, no host volume mounts) with all required services.

---

## 4. Claude Code GitHub Actions (Issue-to-PR Workflow)

The preview orchestrator is one half of the product building workflow. The other half is **Claude Code running in GitHub Actions**, which turns issues into implementation plans and approved plans into PRs. The preview orchestrator then picks up those PRs via webhook.

### 4.1 End-to-End Flow

```
1. Human opens GitHub issue describing a feature/fix
2. Human mentions @claude in the issue (or issue triggers workflow automatically)
3. Claude Code (in GitHub Actions) analyzes the issue and posts an implementation plan as a comment
4. Human reviews and approves the plan (e.g. by commenting "approved" or "@claude implement this")
5. Claude Code implements the plan and opens a PR
6. PR webhook fires → preview orchestrator creates preview
7. Human reviews code + live preview, iterates with @claude in PR comments if needed
8. Human merges PR → preview orchestrator tears down preview
```

### 4.2 Setup per Target Repo

Each target repo needs the following to enable the Claude Code workflow:

**1. Install the Claude GitHub App**

Install from [https://github.com/apps/claude](https://github.com/apps/claude) on each target repo. This app provides the identity Claude uses to comment and push. It requires:

- **Contents**: Read & Write (to modify repository files)
- **Issues**: Read & Write (to respond to issues)
- **Pull requests**: Read & Write (to create PRs and push changes)

**2. Add Anthropic API key as a repository secret**

Add `ANTHROPIC_API_KEY` to the target repo's GitHub Actions secrets (Settings → Secrets and variables → Actions).

**3. Add the Claude Code workflow file**

Create `.github/workflows/claude.yml` in the target repo:

```yaml
name: Claude Code

permissions:
  contents: write
  pull-requests: write
  issues: write

on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
  issues:
    types: [opened, assigned]

jobs:
  claude:
    if: |
      (github.event_name == 'issue_comment' && contains(github.event.comment.body, '@claude')) ||
      (github.event_name == 'pull_request_review_comment' && contains(github.event.comment.body, '@claude')) ||
      (github.event_name == 'issues' && contains(github.event.issue.body, '@claude'))
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: anthropics/claude-code-action@v1
        with:
          anthropic_api_key: ${{ secrets.ANTHROPIC_API_KEY }}
```

**4. Create a CLAUDE.md in the target repo**

This file guides Claude's behavior — coding standards, review criteria, project-specific rules, architectural constraints. Claude reads it automatically when working in the repo.

### 4.3 Relationship to Preview Orchestrator

The two systems are **fully decoupled**:

- Claude Code knows nothing about the preview orchestrator. It simply opens PRs.
- The preview orchestrator knows nothing about Claude Code. It simply reacts to PR webhooks.
- The webhook on the target repo fires for **any** PR — whether created by Claude, by a human, or by any other automation.

This means:

- Previews work for manually created PRs too (not just Claude-authored ones)
- Claude Code can be configured, upgraded, or replaced independently
- The Anthropic API key and the preview orchestrator's GitHub PAT are separate credentials with separate scopes

### 4.4 Credentials Summary per Target Repo

| Credential | Purpose | Stored in | Used by |
|------------|---------|-----------|---------|
| `ANTHROPIC_API_KEY` | Claude Code API access | GitHub repo secrets (managed via OpenTofu) | Claude Code GitHub Action |
| Fine-grained GitHub PAT (bot user) | Tarball download, PR comments, webhook management | AWS Secrets Manager | Preview orchestrator |
| Webhook secret | Webhook HMAC validation | AWS Secrets Manager + GitHub webhook config | Preview orchestrator |
| Claude GitHub App | Claude's identity for pushing code, commenting | Installed on repo | Claude Code GitHub Action |

---

## 5. Quality & Developer Experience (Orchestrator)

### 5.1 Tooling

| Concern | Tool | Config |
|---------|------|--------|
| Task runner | mise | `mise.toml` |
| Linting (Go) | golangci-lint | `.golangci.yml` — govet, staticcheck, errcheck, revive, gosec, funlen, cyclop |
| Formatting (Go) | gofmt, goimports | via golangci-lint |
| Architecture tests | archtest | `tools/archtest/` — vertical boundary enforcement |
| Unit tests | `go test` | min 60% coverage, exclude thin layers |
| Integration tests | `go test -tags=integration` | requires Docker socket |
| Hot reload | air | `.air.toml` |
| CI | GitHub Actions | `.github/workflows/ci.yml` |
| Security | govulncheck | in CI pipeline |

### 5.2 Mise Tasks

```
mise run setup          # Build containers, run checks
mise run dev            # Hot reload development
mise run quality        # Lint, format check, archtest
mise run tests          # Unit tests with coverage
mise run tests-integ    # Integration tests (needs Docker)
mise run security       # govulncheck
mise run all-checks     # Everything
```

### 5.3 CI Pipeline

**Jobs:**

1. **build** — compile, verify no generated drift
2. **quality** — golangci-lint, archtest, gofmt
3. **test** — unit tests with coverage threshold
4. **test-integration** — integration tests with Docker
5. **security** — govulncheck

### 5.4 Documentation

| Document | Content |
|----------|---------|
| `docs/archbook.md` | Vertical slices, boundary rules, state machine, event flow |
| `docs/techbook.md` | Stack decisions: Go, SQLite, Traefik, Docker socket, tarball fetch |
| `docs/devbook.md` | Dev setup, mise tasks, testing, coverage |
| `docs/runbook.md` | Provisioning, re-provisioning, monitoring, troubleshooting |

---

## 6. MVP Delivery Phases

### Phase 1 — Infrastructure Foundation

**Goal:** A running EC2 instance with Traefik serving `*.productbuilder.luminor-tech.net`.

**Tasks:**

1. Create `infrastructure-mgmt/bootstrap/` OpenTofu project
   - S3 bucket + DynamoDB lock table
   - Secrets Manager secret (placeholder values)
2. Create `infrastructure-mgmt/main/` OpenTofu project
   - Route53 hosted zone + wildcard record
   - EC2 + EIP + security group + IAM role
   - Cloud-init template
3. Manual step: NS delegation at apex domain hoster
4. Manual step: populate Secrets Manager values
5. `tofu apply` — verify instance boots, Traefik starts, wildcard cert is issued
6. Verify: `https://api.productbuilder.luminor-tech.net` returns Traefik 404

### Phase 2 — Orchestrator Skeleton

**Goal:** A Go app that receives webhooks and serves a dashboard, deployed via Compose.

**Tasks:**

1. Initialize Go module, project structure, platform layer
2. SQLite database + migrations
3. Config loading (env-based, twelve-factor)
4. HTTP server with healthz endpoint
5. Webhook endpoint: validate signature, parse PR events, log them
6. Dashboard: simple page listing preview records from DB
7. Dockerfile (multi-stage, distroless)
8. Orchestrator Compose stack (orchestrator + Traefik)
9. Deploy to EC2, verify webhook reception from GitHub

### Phase 3 — Preview Lifecycle

**Goal:** Fully working preview creation, update, and deletion.

**Tasks:**

1. Preview domain: entity, state machine, repository (SQLite)
2. Tarball download via GitHub API
3. Preview contract parser (`preview.yml`)
4. Compose override generator (Traefik labels, network, resource limits)
5. Docker Compose execution (`build`, `up`, `down`)
6. Health check polling with timeout
7. PR comment creation and update via GitHub API
8. End-to-end flow: PR opened on etfg-app-starter-kit -> preview live at `pr-{n}.productbuilder.luminor-tech.net`

### Phase 4 — Resilience & Polish

**Goal:** Self-healing, operational visibility, quality gates.

**Tasks:**

1. Reconciliation loop (startup + periodic)
2. Idempotent webhook handling (duplicate events)
3. Graceful handling of concurrent events for same PR
4. Error reporting: meaningful failure messages in PR comments
5. Resource cleanup: orphaned containers, networks, volumes
6. Dashboard: show logs, status history, deployed SHAs
7. Quality: golangci-lint, archtest, coverage threshold, CI pipeline
8. Documentation: archbook, techbook, devbook, runbook

### Phase 5 — etfg-app-starter-kit Integration

**Goal:** The target repo has its preview contract and Claude Code workflow, tested end-to-end.

**Tasks (preview contract):**

1. Create `preview.yml` in etfg-app-starter-kit
2. Create `docker-compose.preview.yml` in etfg-app-starter-kit (prod Dockerfile, all services, preview-appropriate env)
3. Add `/healthz` endpoint verification
4. Open a test PR, verify full preview lifecycle

**Tasks (Claude Code GitHub Actions):**

5. Run `onboard-target.sh` for etfg-app-starter-kit (creates tfvars entry), then `tofu apply` (creates webhook, Secrets Manager entry, `ANTHROPIC_API_KEY` GitHub Actions secret)
6. Install the Claude GitHub App ([github.com/apps/claude](https://github.com/apps/claude)) on etfg-app-starter-kit (manual, one-time)
7. Create `.github/workflows/claude.yml` in etfg-app-starter-kit (see section 4.2)
8. Create `CLAUDE.md` in etfg-app-starter-kit with project conventions, architecture rules, coding standards
9. Test the full workflow: open issue → @claude drafts plan → approve → Claude opens PR → preview deployed → review code + live preview

**Tasks (documentation):**

10. Document the preview contract pattern for future repos
11. Document the Claude Code onboarding recipe for future repos

---

## 7. Key Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Docker build is slow on EC2 | Use BuildKit, layer caching. Future: pre-built images from CI. |
| Heavy preview stacks exhaust instance memory | Resource limits per preview. Monitor and right-size instance type. |
| Webhook events arrive during deployment | Queue events per PR, process sequentially per PR, concurrently across PRs. |
| Instance replacement loses running previews | Reconciliation loop rebuilds everything from GitHub state. |
| Stale previews accumulate | Periodic reconciliation + cleanup of Compose projects not in DB. |
| Wildcard cert rate limits (Let's Encrypt) | One wildcard cert covers all subdomains — no per-preview certs. |
| etfg-app-starter-kit Ollama model download is slow | Pre-pull model in preview Compose setup or accept first-deploy latency. |

---

## 8. Future Considerations (Not in MVP)

- GitHub Deployment/Environment status reporting
- Image-based deployment from CI (Path B from design brief)
- Ephemeral GitHub credentials via broker pattern
- Preview authentication (basic auth or OAuth proxy)
- Firecracker microVM isolation
- Preview expiration / TTL
- Notification channels beyond PR comments (Slack, etc.)
