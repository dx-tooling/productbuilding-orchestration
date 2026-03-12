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
| Secrets (repo) | age encryption (asymmetric) | Encrypted in git, only `.key` from 1Password needed |
| Secrets (runtime) | AWS Secrets Manager | Automated provisioning, no manual SCP |
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
    providers.tf
    terraform.tfstate     # Local (committed to .gitignore)
  main/                   # Ongoing management, S3 backend
    backend.tf            # S3 + DynamoDB backend config
    main.tf               # EC2, EIP, SG, Route53, IAM
    targets.tf            # Per-target-repo resources (webhooks, secrets, GH Actions secrets)
    variables.tf
    outputs.tf
    cloud-init.yml        # Instance bootstrap script (templatefile)
    providers.tf          # AWS + GitHub providers
    terraform.tfvars      # Gitignored — decrypted from secrets/
    targets.auto.tfvars   # Gitignored — decrypted from secrets/

secrets/                    # age-encrypted secrets (committed to git)
  README.md                 # Setup instructions (1Password link, age install)
  public-key.txt            # age public key (committed — anyone can encrypt)
  .key                      # age secret key (GITIGNORED — from 1Password)
  secrets.yaml.enc          # AWS creds, management PAT → decrypts to secrets.yaml
  terraform.tfvars.enc      # mgmt PAT, SSH key → decrypts to infrastructure-mgmt/main/terraform.tfvars
  targets.auto.tfvars.enc   # target repo credentials → decrypts to infrastructure-mgmt/main/targets.auto.tfvars

.mise/
  lib/
    load-secrets          # Helper: loads AWS + GitHub credentials from secrets.yaml
  tasks/
    instance-start        # Start EC2 instance
    instance-stop         # Stop EC2 instance
    instance-status       # Show instance state and IP
    ssh                   # SSH into the instance
    infra-plan            # tofu plan
    infra-apply           # tofu apply
    infra-output          # tofu output
    onboard-target        # Interactive: add a new target repo
    deploy                # Deploy latest code to EC2

orchestrator-app/           # Go application source (see section 3.1)
docker-compose.yml          # Production stack (Traefik + orchestrator, runs on EC2)
docker-compose.dev.yml      # Dev stack (source mount + air, runs locally)
mise.toml                   # Task runner config
encrypt-secrets.sh          # Encrypt plaintext secrets → secrets/*.enc
decrypt-secrets.sh          # Decrypt secrets/*.enc → plaintext files
```

### 2.2 Bootstrap Project (`infrastructure-mgmt/bootstrap/`)

Creates the prerequisites for the main project. Run once manually with local state.

**Resources:**

- S3 bucket for OpenTofu state (versioning enabled)
- DynamoDB table for state locking
- AWS Secrets Manager secrets (one per target repo, created by main project — bootstrap only creates the bucket/table)

**Recipe:**

1. `./decrypt-secrets.sh` (decrypts `secrets.yaml`, `terraform.tfvars`, `targets.auto.tfvars`)
2. `cd infrastructure-mgmt/bootstrap`
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
- **EC2 instance**: Ubuntu 24.04, variable instance type (default `t3.xlarge`)
- **Per-target-repo resources** (via `for_each` over targets):
  - Secrets Manager secret: `productbuilder/targets/{repo_name}` with keys `github_pat`, `webhook_secret`
  - GitHub webhook: PR events, pointing to `https://api.productbuilder.luminor-tech.net/webhook`
  - GitHub Actions secret: `ANTHROPIC_API_KEY` on the target repo (for Claude Code)

**Target registry** (`infrastructure-mgmt/main/targets.auto.tfvars`, gitignored):

```hcl
targets = {
  "etfg-app-starter-kit" = {
    repo_owner         = "luminor-project"
    repo_name          = "etfg-app-starter-kit"
    github_pat         = "github_pat_..."
    webhook_secret     = "a1b2c3..."          # openssl rand -hex 32
    fireworks_api_key  = "fw-..."              # for OpenCode GitHub Actions (FireworksAI)
  }
  # Add future target repos here
}
```

OpenTofu iterates over this map to create webhooks, Secrets Manager entries, and GitHub Actions secrets per target. Onboarding a new repo = run `mise run onboard-target` + `tofu apply`.

### 2.4 Target Repo Onboarding Script

`.mise/tasks/onboard-target` (run via `mise run onboard-target`) automates adding a new target repo. It handles both the orchestrator-side registration **and** scaffolding the target repo itself:

```
$ mise run onboard-target

Target repo onboarding
======================
Repo owner [luminor-project]: luminor-project
Repo name: my-new-app
GitHub PAT (fine-grained, scoped to this repo): github_pat_...
Fireworks API key (for OpenCode): fw-...
Local repo clone path: /Users/me/git/my-new-app
Generating webhook secret... done (e4f8a1...)

Appending to main/targets.auto.tfvars... done.

Scaffolding target repo at /Users/me/git/my-new-app...
  Created .productbuilding/preview/config.yml (edit to match your app)
  Created .productbuilding/preview/docker-compose.yml (edit to match your app)
  Created .github/workflows/opencode.yml
  Created AGENTS.md (starter template — customize for your project)

Next steps:
  1. mise run infra-apply
  2. Install the OpenCode GitHub App on the repo:
     https://github.com/apps/opencode-agent
  3. Review and customize the scaffolded files, then commit and push
```

The script:

1. Prompts for repo owner, repo name, fine-grained PAT, Fireworks API key, **local clone path**
2. Generates a webhook secret via `openssl rand -hex 32`
3. Appends the new target entry to `main/targets.auto.tfvars`
4. Scaffolds productbuilding files in the target repo:
   - `.productbuilding/preview/config.yml` — preview contract (starter template)
   - `.productbuilding/preview/docker-compose.yml` — preview Compose file (starter template)
   - `.github/workflows/opencode.yml` — OpenCode workflow
   - `AGENTS.md` — OpenCode project guide (starter template)
5. Prints remaining manual steps (OpenCode App install, review + commit scaffolded files)

The **one step that cannot be automated** is installing the OpenCode GitHub App — it requires an OAuth consent flow in the browser. The script prints the direct link.

**Cloud-init script** (templated by OpenTofu, receives region/domain/zone ID/repo clone URL):

1. Install Docker Engine + Docker Compose plugin, add `ubuntu` user to `docker` group
2. Create `preview-net` external Docker network
3. Clone the orchestration repo (using management PAT embedded in clone URL)
4. Write `.env` file with `AWS_REGION`, `AWS_HOSTED_ZONE_ID`, `PREVIEW_DOMAIN`
5. Fetch target secrets from AWS Secrets Manager → `/opt/orchestrator/targets.json`
6. `docker compose up -d` the orchestrator stack (Traefik + orchestrator)

On a fresh provision, Traefik auto-provisions the wildcard Let's Encrypt cert via DNS-01 + Route53 (using the instance's IAM role).

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

All Go application source lives under `orchestrator-app/`, keeping infrastructure and app code cleanly separated. No Go toolchain is needed on the host — everything runs inside the dev Docker container via `mise run app-*` tasks.

```
orchestrator-app/             # Go application source
  cmd/
    server/
      main.go                 # Entry point, DI wiring

  internal/
    preview/                  # Core domain vertical
      domain/
        preview.go            # Preview entity, state machine
        statemachine.go       # State transition validation
        service.go            # Preview lifecycle logic
        repository.go         # Repository interface
        service_test.go
      facade/
        dto.go                # DTOs for cross-vertical use
      infra/
        sqlite_repository.go
      web/
        handlers.go           # GET /previews
        routes.go
      testharness/
        factories.go

    github/                   # GitHub integration vertical
      domain/
        webhook.go            # Webhook payload parsing + signature validation
        webhook_test.go
      facade/
        dto.go                # PREvent DTO
      web/
        handlers.go           # POST /webhook
        routes.go

    orchestration/            # Docker/Compose management vertical (Phase 3)
      domain/
        deployer.go           # Compose project lifecycle
        healthcheck.go        # Container health polling
        labels.go             # Traefik label generation
        service.go
      facade/
        dto.go                # DeploymentResult, ContainerStatus, etc.
      infra/
        compose.go            # Shell out to docker compose

    dashboard/                # Web UI vertical
      web/
        handlers.go           # GET / (dashboard page, html/template)
        routes.go

    platform/                 # Shared infrastructure
      config/                 # App configuration (env-based, caarlos0/env)
      database/               # SQLite connection (modernc.org/sqlite), migrations
      logging/                # Structured logging (slog)
      server/                 # HTTP server with graceful shutdown

  migrations/
    001_create_previews.up.sql
    001_create_previews.down.sql

  docker/
    dev/
      Dockerfile              # golang:1.26 + air, for local dev
    prod/
      Dockerfile              # Multi-stage: build + distroless runtime

  go.mod
  .air.toml                   # Hot reload config

docker-compose.yml            # Production stack (Traefik + orchestrator, runs on EC2)
docker-compose.dev.yml        # Dev stack (source mount + air, runs locally)

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
  "repo_owner": "luminor-project",
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
8. Read `.productbuilding/preview/config.yml` contract from extracted source
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

#### Logs Endpoint

The orchestrator exposes a logs endpoint for debugging preview deployments:

**Endpoint:** `GET /previews/{owner}/{repo}/{pr}/logs`

**Query Parameters:**
- `tail` — Number of lines to return (default: 100)
- `follow` — Stream logs in real-time (default: false)

**Examples:**
```bash
# Get last 100 lines
curl "https://api.productbuilder.luminor-tech.net/previews/luminor-project/myrepo/5/logs"

# Get last 500 lines
curl "https://api.productbuilder.luminor-tech.net/previews/luminor-project/myrepo/5/logs?tail=500"

# Stream logs in real-time
curl "https://api.productbuilder.luminor-tech.net/previews/luminor-project/myrepo/5/logs?follow=true"
```

The logs endpoint is also linked in PR comments as "View Logs" for easy access.

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
# Note: Traefik uses the EC2 instance's IAM role for Route53 access (DNS-01 challenge),
# so no AWS_ACCESS_KEY_ID/SECRET_ACCESS_KEY needed — only region and hosted zone ID.

services:
  traefik:
    image: traefik:v3.6
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
      - "--api.dashboard=true"
      - "--log.level=INFO"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - letsencrypt:/letsencrypt
    networks:
      - preview-net
    environment:
      - AWS_REGION=${AWS_REGION:-eu-central-1}
      - AWS_HOSTED_ZONE_ID=${AWS_HOSTED_ZONE_ID}
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.traefik-dashboard.rule=Host(`api.${PREVIEW_DOMAIN}`) && (PathPrefix(`/api`) || PathPrefix(`/dashboard`))"
      - "traefik.http.routers.traefik-dashboard.entrypoints=websecure"
      - "traefik.http.routers.traefik-dashboard.tls.certresolver=le"
      - "traefik.http.routers.traefik-dashboard.service=api@internal"
    restart: unless-stopped

  # orchestrator service will be added in Phase 2:
  # orchestrator:
  #   build:
  #     context: .
  #     dockerfile: docker/prod/Dockerfile
  #   volumes:
  #     - /var/run/docker.sock:/var/run/docker.sock
  #     - orchestrator-data:/data          # SQLite DB
  #     - workspaces:/opt/orchestrator/workspaces  # Extracted tarballs
  #   networks:
  #     - preview-net
  #   labels:
  #     - "traefik.enable=true"
  #     - "traefik.http.routers.orchestrator.rule=Host(`api.${PREVIEW_DOMAIN}`) && PathPrefix(`/`)"
  #     - "traefik.http.routers.orchestrator.entrypoints=websecure"
  #     - "traefik.http.routers.orchestrator.tls.certresolver=le"
  #     - "traefik.http.services.orchestrator.loadbalancer.server.port=8080"
  #   environment:
  #     - DATABASE_PATH=/data/orchestrator.db
  #     - TARGETS_CONFIG_PATH=/data/targets.json
  #     - PREVIEW_DOMAIN=${PREVIEW_DOMAIN}
  #     - WORKSPACE_DIR=/opt/orchestrator/workspaces
  #     - AWS_REGION=${AWS_REGION:-eu-central-1}
  #   restart: unless-stopped

networks:
  preview-net:
    external: true

volumes:
  letsencrypt:
  # orchestrator-data:   # Phase 2
  # workspaces:          # Phase 2
```

### 3.7 Generated Preview Compose Override

The orchestrator generates a `docker-compose.override.yml` for each preview, adding Traefik integration and resource limits:

```yaml
# Generated for etfg-app-starter-kit PR 42, placed alongside the repo's .productbuilding/preview/docker-compose.yml

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

Target repos use a `.productbuilding/` directory for all product building integration files. This is future-proof: additional aspects beyond preview (e.g., seeding, benchmarking) can live as sibling directories.

```
.productbuilding/
  preview/
    config.yml              # Preview contract — how to build and run a preview
    docker-compose.yml      # Production-oriented Compose file for previews
```

Example `.productbuilding/preview/config.yml` for etfg-app-starter-kit:

```yaml
version: 1

compose:
  file: .productbuilding/preview/docker-compose.yml
  service: app

runtime:
  internal_port: 8080
  healthcheck_path: /healthz
  startup_timeout_seconds: 300

database:
  migrate_command: go run ./cmd/migrate business up && go run ./cmd/migrate rag up
```

The target repo also provides `.productbuilding/preview/docker-compose.yml` — a production-oriented Compose file (using the prod Dockerfile, no hot reload, no host volume mounts) with all required services.

---

## 4. Claude Code GitHub Actions (Issue-to-PR Workflow)

The preview orchestrator is one half of the product building workflow. The other half is **OpenCode running in GitHub Actions**, which turns issues into implementations and opens PRs. The preview orchestrator then picks up those PRs via webhook.

### 4.1 End-to-End Flow

```
1. Human opens GitHub issue describing a feature/fix
2. Human comments `/opencode` in the issue
3. OpenCode (in GitHub Actions) analyzes the issue and implements the feature, opening a PR
4. PR webhook fires → preview orchestrator creates preview
5. Human reviews code + live preview, iterates with `/opencode` in PR comments if needed
6. Human merges PR → preview orchestrator tears down preview
```

### 4.2 Setup per Target Repo

Each target repo needs the following to enable the OpenCode workflow:

**1. Install the OpenCode GitHub App**

Install from [https://github.com/apps/opencode-agent](https://github.com/apps/opencode-agent) on each target repo. This app provides the identity OpenCode uses to comment and push. It requires:

- **Contents**: Read & Write (to modify repository files)
- **Issues**: Read & Write (to respond to issues)
- **Pull requests**: Read & Write (to create PRs and push changes)

**2. Add Fireworks API key as a repository secret**

Add `FIREWORKS_API_KEY` to the target repo's GitHub Actions secrets (Settings → Secrets and variables → Actions).

**3. Add the OpenCode workflow file**

Create `.github/workflows/opencode.yml` in the target repo:

```yaml
name: OpenCode

permissions:
  contents: write
  pull-requests: write
  issues: write
  id-token: write

on:
  issue_comment:
    types: [created]
  pull_request_review_comment:
    types: [created]
  pull_request_review:
    types: [submitted]

jobs:
  opencode:
    if: |
      contains(github.event.comment.body, '/opencode')
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: anomalyco/opencode/github@latest
        env:
          FIREWORKS_API_KEY: ${{ secrets.FIREWORKS_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          model: fireworks-ai/accounts/fireworks/models/kimi-k2p5
```

**4. Create an AGENTS.md in the target repo**

This file guides OpenCode's behavior — coding standards, review criteria, project-specific rules, architectural constraints. OpenCode reads it automatically when working in the repo.

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

App development tasks (all run inside the Docker container — no host Go needed):

```
mise run app-setup      # Build containers, download deps, run tests, start dev
mise run app-dev        # Hot reload development (air)
mise run app-build      # Build Go binary
mise run app-quality    # go vet, gofmt check
mise run app-tests      # Unit tests with race detector
mise run app-exec ...   # Run any command in the app container
mise run app-compose .. # Run docker compose for the dev stack
```

Infrastructure tasks (existing):

```
mise run infra-plan     # OpenTofu plan
mise run infra-apply    # OpenTofu apply
mise run infra-output   # OpenTofu output
mise run instance-start # Start EC2 instance
mise run instance-stop  # Stop EC2 instance
mise run instance-status # Show instance state
mise run ssh            # SSH into instance
mise run onboard-target # Add new target repo
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
2. Create `infrastructure-mgmt/main/` OpenTofu project
   - Route53 hosted zone + wildcard record
   - EC2 + EIP + security group + IAM role
   - Cloud-init template
3. Manual step: NS delegation at apex domain hoster
4. `tofu apply` — verify instance boots, Traefik starts, wildcard cert is issued
5. Verify: `https://api.productbuilder.luminor-tech.net` returns Traefik 404

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
3. Preview contract parser (`.productbuilding/preview/config.yml`)
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

**Goal:** The target repo has its preview contract and OpenCode workflow, tested end-to-end.

**Tasks (onboarding + infra):**

1. Run `mise run onboard-target` for etfg-app-starter-kit — creates tfvars entry and scaffolds `.productbuilding/preview/`, `.github/workflows/opencode.yml`, `AGENTS.md` in the target repo
2. `mise run infra-apply` — creates webhook, Secrets Manager entry, `FIREWORKS_API_KEY` GitHub Actions secret
3. Install the OpenCode GitHub App ([github.com/apps/opencode-agent](https://github.com/apps/opencode-agent)) on etfg-app-starter-kit (manual, one-time)

**Tasks (preview contract):**

4. Customize `.productbuilding/preview/config.yml` for etfg-app-starter-kit (ports, healthcheck, migrations)
5. Customize `.productbuilding/preview/docker-compose.yml` for etfg-app-starter-kit (prod Dockerfile, all services, preview-appropriate env)
6. Add `/healthz` endpoint verification
7. Open a test PR, verify full preview lifecycle

**Tasks (OpenCode):**

8. Customize `AGENTS.md` in etfg-app-starter-kit with project conventions, architecture rules, coding standards
9. Test the full workflow: open issue → comment `/opencode` → OpenCode responds → OpenCode opens PR → preview deployed → review code + live preview

**Tasks (documentation):**

10. Document the preview contract pattern for future repos
11. Document the OpenCode onboarding recipe for future repos

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

## 8. Troubleshooting

### "Webhook from unknown repo" error

**Symptom:** Orchestrator logs show:
```
{"level":"WARN","msg":"webhook from unknown repo","repo":"owner/new-repo"}
```

**Cause:** The orchestrator loads target configuration at startup and caches it in memory. When you add a new target via `mise run infra-apply`, the running orchestrator doesn't automatically pick up the new configuration.

**Solution:** Redeploy the orchestrator:
```bash
mise run deploy
```

**Prevention:** Always run `mise run deploy` after adding new targets:
```bash
mise run onboard-target     # Add target
mise run secrets-encrypt     # Encrypt
mise run infra-apply         # Create webhook/secret
mise run deploy              # CRITICAL: Reload orchestrator
```

### OpenCode workflow fails with API errors

**Symptom:** OpenCode action fails with `undefined is not an object (evaluating 'octoRest.rest')`

**Cause:** The workflow is missing `GITHUB_TOKEN` environment variable.

**Solution:** Ensure the workflow includes:
```yaml
- uses: anomalyco/opencode/github@latest
  env:
    FIREWORKS_API_KEY: ${{ secrets.FIREWORKS_API_KEY }}
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}  # Required for API access
```

### Preview not deploying after PR update

**Symptom:** Pushing commits to an existing PR doesn't trigger preview updates.

**Cause:** Webhook may not be configured for "synchronize" events.

**Solution:** Verify webhook is configured for `pull_request` events (includes synchronize). Check GitHub webhook settings or re-run `mise run infra-apply`.

---

## 9. Future Considerations (Not in MVP)

- GitHub Deployment/Environment status reporting
- Image-based deployment from CI (Path B from design brief)
- Ephemeral GitHub credentials via broker pattern
- Preview authentication (basic auth or OAuth proxy)
- Firecracker microVM isolation
- Preview expiration / TTL
- Notification channels beyond PR comments (Slack, etc.)
