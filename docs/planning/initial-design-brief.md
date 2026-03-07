# PR Preview Platform + GitHub/Claude Workflow

## Engineering Design Brief

## 1. Purpose

Build a multi-stage workflow around GitHub issues and pull requests in which:

- A new GitHub issue triggers an agent-assisted planning step.
- The agent posts an implementation plan back into the issue.
- A human explicitly approves the plan.
- The agent implements the plan and opens a PR.
- A preview system continuously deploys the latest code state of that PR to a stable human-facing URL.
- The human reviewer uses that live preview during PR review.

The immediate implementation target is only the PR preview system. The broader issue-to-plan-to-PR workflow is part of the architectural context and should inform integration design, but not expand the MVP scope.

Claude Code's GitHub Actions integration is intended to run inside GitHub Actions runner workspaces, typically on GitHub-hosted runners. That means agent-side code editing and PR creation are expected to happen in CI runner environments, while the preview system is a downstream runtime/review environment.

## 2. MVP Scope

### In scope

Build a preview platform that:

- Creates one stable preview per open PR
- Always deploys the latest head commit of that PR
- Exposes the preview at a stable URL such as `https://pr-123-preview.example.com`
- Updates the existing preview when the PR receives new commits
- Removes the preview when the PR is closed or merged
- Reports preview state back into GitHub
- Works on a root server under our control with Docker already available

### Out of scope for now

Do not implement yet:

- Agent-specific preview environments
- SSH or CLI control surfaces
- Multi-profile preview types
- Per-commit immutable public preview URLs
- Automated browser validation environments for agents
- Generalized multi-tenant public preview hosting
- Support for repo hosts other than GitHub
- Full issue-to-plan-to-PR workflow orchestration

## 3. Core Product Decision

The preview platform should be **PR-centric, not commit-centric**.

That means:

- The stable public identity is the PR number
- The deployed revision is the PR's current head SHA
- The URL remains stable across updates
- The deployment content changes as the PR branch changes

**Example:**

- Stable URL: `https://pr-123-preview.example.com`
- Currently deployed SHA: `2ea4d41b...`

This is preferable for human review because the link remains stable while the preview tracks the latest PR state.

## 4. Architectural Principle: Bilateral Responsibility

The system should follow a bilateral contract:

**The preview orchestrator knows:**

- How to receive preview deployment triggers
- How to map PRs to stable URLs
- How to fetch/build/deploy/update/delete previews
- How to expose them through routing/TLS
- How to monitor readiness
- How to report status back to GitHub

**Each target application knows:**

- How it should be built
- Which services it requires
- What runtime configuration it needs
- Whether migrations/seeding are required
- How readiness should be determined

This keeps app-specific operational knowledge inside the repo while centralizing preview lifecycle logic in one platform.

## 5. GitHub Context and Constraints

GitHub natively supports webhooks and GitHub Actions triggers. GitHub Actions workflows are event-driven and can be triggered by `issue`, `issue-comment`, and `pull-request` events.

GitHub also supports deployment and environment concepts that are suitable for attaching a machine-readable preview URL to a deployment. Deployment statuses are part of the REST API, and GitHub environments/deployments are the correct native surface for publishing preview runtime information.

GitHub does not natively provide a generic one-off "callback URL with credentials" in webhook payloads for posting comments or deployment updates. If temporary outbound GitHub credentials are desired, they must be derived by our own integration layer, likely via a GitHub App or broker pattern. This matters for integration design, but does not block the preview MVP.

## 6. End-to-End Workflow Context

The broader intended workflow is:

1. GitHub issue opened.
2. Claude Code, via GitHub Actions, drafts an implementation plan and posts it to the issue.
3. Human approves the plan.
4. Claude Code implements and opens a PR.
5. Preview platform creates or updates the PR preview.
6. Human reviews both:
   - Code in the PR
   - Running application via preview URL

The preview platform should therefore be designed as a clean downstream component in a larger automation chain.

## 7. Target User Experience

For each open PR, GitHub should show a stable live preview.

A human reviewer should be able to:

- Open the PR
- See preview status
- Click a stable preview URL
- View the latest running version of the PR branch
- Trust that the preview corresponds to the PR's latest head commit

The preview system should ideally report:

- **Pending** while building
- **Ready** with URL when healthy
- **Failed** with concise reason when unsuccessful

## 8. System Components

### A. Preview Orchestrator

A central service that manages preview lifecycle.

**Responsibilities:**

- Receive PR-related deployment triggers
- Maintain preview state
- Fetch target repo at exact PR head SHA
- Execute app-provided preview contract
- Allocate/update/remove preview runtime
- Monitor readiness
- Report state to GitHub
- Garbage-collect resources

### B. Reverse Proxy / Router

**Responsible for:**

- Wildcard subdomain routing
- TLS termination
- Forwarding requests to the correct preview container/service

Traefik is a strong fit because it integrates well with Docker label-based service discovery.

### C. Docker Runtime

Each preview runs as an isolated Docker stack, ideally one Compose project per PR.

### D. State Store

A small database for orchestrator state, e.g. Postgres or SQLite.

**Track at minimum:**

- Repo
- PR number
- Branch
- Current head SHA
- Stable preview URL
- Deployment status
- Compose project name / runtime identifiers
- Timestamps
- Error summaries

### E. GitHub Integration Layer

Initial MVP can be either:

- GitHub Actions calling the preview orchestrator explicitly
- GitHub webhook receiver inside the orchestrator
- A thin broker between GitHub and orchestrator

This layer should stay replaceable.

## 9. Trigger Model

### Preferred preview lifecycle events

The platform should react to:

- `pull_request.opened`
- `pull_request.reopened`
- `pull_request.synchronize`
- `pull_request.closed`

### Desired behavior

| Event                | Action                                |
|----------------------|---------------------------------------|
| `opened` / `reopened` | Create preview                       |
| `synchronize`        | Update preview to latest PR head SHA  |
| `closed`             | Destroy preview                       |

### Source of truth

The PR itself is the authoritative preview object.

The orchestrator should identify a preview primarily by:

- Repository
- PR number

...not by branch name alone.

## 10. URL and Naming Strategy

### Public URL

Use PR-number-based stable URLs:

```
https://pr-123-preview.example.com
```

**Reasoning:**

- Stable across pushes
- Independent of branch naming
- Easy to reason about in GitHub UI
- Easy to map operationally

### Internal naming

Example:

- Compose project: `preview_pr_123`
- Docker network: `preview_pr_123_net`
- Internal service labels keyed by PR number

## 11. Deployment Model

### Recommended MVP deployment approach

One isolated Docker Compose project per PR.

For PR 123:

1. Fetch repo at current PR head SHA
2. Prepare env/config
3. Build or pull images
4. Start/replace stack
5. Wait for readiness
6. Route `pr-123-preview.example.com` to it

### Update behavior

On PR updates:

- Redeploy in place to the latest head SHA
- Preserve the stable URL
- Atomically switch traffic only after health checks pass if feasible

### Deletion behavior

On PR close/merge:

- Stop containers
- Remove network/volumes as appropriate
- Remove routing
- Mark preview deleted

## 12. Repo-Level Preview Contract

Each target application should define a small preview contract in the repo.

This is the app-side expression of the bilateral architecture.

### Recommended format

A repo-local manifest such as `preview.yml`, plus Docker Compose and optional scripts.

**Example scope of manifest:**

- Compose file location
- Main app service name
- Internal port
- Readiness endpoint/path
- Startup timeout
- Required env vars
- Optional migrate/seed commands

### Example shape

```yaml
version: 1

runtime:
  internal_port: 3000
  healthcheck_path: /health
  startup_timeout_seconds: 180

compose:
  file: docker-compose.preview.yml
  service: app

database:
  migrate_command: npm run migrate
  seed_command: npm run seed:preview
```

### Design rule

The orchestrator must understand a stable contract, but should not embed app-specific logic.

## 13. GitHub Feedback Model

The preview system should report status back into GitHub using two surfaces:

### A. Deployment / environment-style status

This is the machine-readable source of truth.

Use it to represent:

- `pending`
- `ready`
- `failed`

...and attach the preview URL as the environment/deployment URL where appropriate. GitHub environments and deployment statuses are designed for this kind of integration.

### B. PR comment

This is the human-visible surface.

**Recommended behavior:**

- Maintain one preview status comment per PR
- Update it rather than creating new comments on every redeploy

**Example states:**

- "Preview building..."
- "Preview ready: ..."
- "Preview failed: ..."

This avoids comment spam while keeping the status obvious to reviewers.

## 14. Authentication and Trust Boundary

### Immediate requirement

The preview system may eventually need to talk back to GitHub via API, but should avoid long-lived repo-host credentials where practical.

### Recommended longer-term direction

Use an integration pattern where:

- GitHub or a trusted broker triggers the orchestrator
- The orchestrator receives only short-lived, scoped credentials for the needed callback/update operations
- Long-lived GitHub secrets remain outside the orchestrator where possible

### MVP flexibility

This does not need to be perfected in the first iteration. The platform should simply be designed so that GitHub API auth can later be supplied ephemerally rather than being hardcoded into the preview service.

## 15. Build Strategy

### Path A: Build on preview server

Simplest initial implementation.

1. Fetch repo
2. Build locally with Docker/BuildKit
3. Deploy locally

**Pros:** Fewer moving parts; simple first milestone.

**Cons:** Slower redeploys; more load on preview host.

### Path B: Build in CI, deploy by image tag

Future target.

1. CI builds image for PR head SHA
2. Pushes image to registry
3. Orchestrator pulls and runs by tag

**Pros:** Faster preview deployment; better cache use; cleaner separation of build and runtime.

**Cons:** More infrastructure coordination.

### Recommendation

Start with local build-on-server if needed for speed of implementation, but design interfaces so image-based deployment can replace it later.

## 16. Health, Readiness, and Update Safety

A preview should be considered ready only after:

- Containers are started
- Migrations/seeding are complete if applicable
- Application health check passes

The orchestrator should support explicit deployment states:

- `pending`
- `building`
- `deploying`
- `ready`
- `failed`
- `deleted`

If an update fails:

- The failure should be reported to GitHub
- The platform should preserve enough diagnostic detail for operators
- Whether the previous healthy preview remains live or not should be a deliberate design choice

**Recommendation:** Prefer keeping the previous healthy version live until the new one is verified, if implementation complexity permits.

## 17. Security Requirements

At minimum:

- Wildcard DNS and TLS for preview subdomains
- Strong isolation between PR previews
- CPU/memory limits on containers
- No production secrets in preview environments
- Sanitized or synthetic data only
- Branch/PR metadata sanitized before use in hostnames or labels
- Robust cleanup of stale resources
- Careful handling of untrusted code if external contributors are in scope

**Important note:** PR previews are code execution. If previews may ever be built from untrusted forks, the threat model changes substantially and must be handled explicitly.

## 18. Operational Requirements

The platform should support:

- Deterministic mapping from PR to preview URL
- Auditability of which SHA is currently deployed
- Idempotent handling of repeated PR events
- Retries for failed transient operations
- Cleanup on PR close
- Periodic reconciliation between DB state and actual Docker state
- Basic logs/metrics sufficient for debugging preview failures

## 19. Minimal Data Model

**Suggested entity: `Preview`**

| Field                | Notes                              |
|----------------------|------------------------------------|
| `id`                 |                                    |
| `repo_owner`         |                                    |
| `repo_name`          |                                    |
| `pr_number`          |                                    |
| `branch_name`        |                                    |
| `head_sha`           |                                    |
| `preview_url`        |                                    |
| `status`             |                                    |
| `project_name`       |                                    |
| `created_at`         |                                    |
| `updated_at`         |                                    |
| `last_successful_sha`|                                    |
| `error_stage`        |                                    |
| `error_message`      |                                    |

**Optional:**

- Deployment metadata
- GitHub comment ID for update-in-place
- GitHub deployment ID / status ID
- Expiration timestamps

## 20. Minimal External Interfaces

### Trigger input

The orchestrator should accept enough data to deploy a PR preview:

- Repo identity
- PR number
- Branch name
- PR head SHA
- Event type
- Optional GitHub callback/auth context

This may arrive via webhook, API call, or broker.

### Orchestrator internal workflow

1. Reconcile desired preview state for that PR
2. Fetch exact code
3. Invoke repo preview contract
4. Expose service at stable PR URL
5. Report result back

### Output to GitHub

- Machine-readable preview status
- Stable preview URL
- Optionally concise failure reason

## 21. Recommended MVP Delivery Plan

### Phase 1

Implement:

- One preview per PR
- Local builds on preview server
- One Compose stack per PR
- Stable URL per PR
- Health checks
- Teardown on PR close

### Phase 2

Add:

- GitHub deployment/environment reporting
- Maintained PR status comment
- Better logs and reconciliation

### Phase 3

Add:

- Image-based deployment from CI
- Ephemeral GitHub API credentials or broker pattern
- Stronger rollout behavior
- Preview auth if required

## 22. Key Non-Goals to Protect the MVP

Do not let the first implementation expand into:

- A generic PaaS
- An agent runtime platform
- A browser automation framework
- A multi-interface control plane
- Per-commit preview archival
- Environment reset/reseed APIs for external consumers

Those may come later, but they are not required to realize the core business value.

## 23. Final Architectural Statement

Build a PR-scoped preview platform that continuously deploys the latest head commit of each open GitHub pull request to a stable human-facing URL, using a central preview orchestrator plus a repo-local preview contract that lets each application define how it is built, started, migrated, seeded, and health-checked.

This platform is one downstream stage in a larger GitHub-based workflow where issues lead to agent-authored plans, human approval, agent-created PRs, and finally human review against both code and a live running preview.
