# Runbook — Setup from Scratch

This documents the full procedure to provision the orchestration infrastructure from zero. Follow these steps whenever you need to set up a fresh environment (new AWS account, disaster recovery, etc.).

## Prerequisites

On your local machine:

- [mise](https://mise.jdx.dev/) installed
- [OpenTofu](https://opentofu.org/) installed (`brew install opentofu`)
- [AWS CLI](https://aws.amazon.com/cli/) installed
- [age](https://github.com/FiloSottile/age) installed (`brew install age`)
- SSH key pair (default: `~/.ssh/id_rsa.pub`)

You also need:

- The age secret key from 1Password (see `secrets/README.md`)
- Access to the DNS management panel for `luminor-tech.net`

## Step 1: Decrypt secrets

Paste the age secret key from 1Password into `secrets/.key`, then decrypt:

```bash
# Paste age secret key (AGE-SECRET-KEY-...) into secrets/.key
mise run secrets-decrypt
```

This produces the gitignored plaintext files: `secrets.yaml`, `infrastructure-mgmt/main/terraform.tfvars`, and `infrastructure-mgmt/main/targets.auto.tfvars`.

## Step 2: Bootstrap (one-time per AWS account)

This creates the S3 bucket and DynamoDB table for OpenTofu state management.

```bash
mise run infra-plan    # Verify — will fail if bootstrap hasn't been run yet
cd infrastructure-mgmt/bootstrap
eval "$(.mise/lib/load-secrets)"
tofu init
tofu apply
```

Note the outputs — the S3 bucket name and DynamoDB table name are hardcoded in `infrastructure-mgmt/main/backend.tf`. If you're starting in a new AWS account, update `backend.tf` with the new bucket name.

## Step 3: Apply main infrastructure

```bash
mise run infra-apply
```

This creates: Route53 hosted zone, wildcard DNS record, Elastic IP, EC2 instance, security groups, IAM role, and SSH key pair.

Note the outputs, especially `route53_nameservers`.

## Step 4: DNS delegation (manual)

**This is the one manual step that cannot be automated.**

At your DNS hoster for `luminor-tech.net`, create NS records to delegate the `productbuilder` subdomain to Route53:

| Record type | Name | Value |
|-------------|------|-------|
| NS | `productbuilder` | `ns-XXXX.awsdns-XX.org` |
| NS | `productbuilder` | `ns-XXX.awsdns-XX.com` |
| NS | `productbuilder` | `ns-XXXX.awsdns-XX.co.uk` |
| NS | `productbuilder` | `ns-XXX.awsdns-XX.net` |

Use the exact nameserver values from `mise run infra-output`.

### Verify DNS delegation

Wait a few minutes for propagation, then verify:

```bash
# Should return the 4 Route53 nameservers
dig NS productbuilder.luminor-tech.net +short

# Should return the Elastic IP
dig A api.productbuilder.luminor-tech.net +short
dig A anything.productbuilder.luminor-tech.net +short
```

## Step 5: Verify instance is healthy

```bash
mise run instance-status    # Should show "running"
mise run ssh -- "docker --version && docker network ls | grep preview-net"
```

## Step 6: Deploy orchestrator

```bash
mise run deploy             # SSH in, git pull, rebuild orchestrator, health check
```

## Step 7: Onboard target repos

For each target repository:

```bash
mise run onboard-target     # Interactive — prompts for repo details, credentials, and local clone path
                            # Registers the target AND scaffolds .productbuilding/, workflow, AGENTS.md
mise run secrets-encrypt     # Re-encrypt updated targets.auto.tfvars
mise run infra-apply        # Creates webhook, Secrets Manager entry, GitHub Actions secret
mise run deploy             # Redeploy orchestrator to load new target (REQUIRED)
```

**Slack integration setup** (if using Slack):

- Ensure `SLACK_SIGNING_SECRET`, `SLACK_WORKSPACE`, and `FIREWORKS_API_KEY` are set on the orchestrator host (managed via Terraform / Secrets Manager / deploy task)
- The Slack app needs these Bot Token Scopes: `chat:write`, `chat:write.public`, `reactions:write`, `reactions:read`, `channels:read`, `channels:history`, `app_mentions:read`, `users:read`
- Enable Event Subscriptions with `app_mention` event, Request URL: `https://api.{domain}/slack/events`
- No slash commands or interactivity URL needed — all interaction is via `@ProductBuilder` mentions
- Slack channels **must** follow the naming convention `#productbuilding-{repo-name}` (the system uses this to match channels to repos)
- After adding scopes or event subscriptions, **reinstall the app** to the workspace

**Usage:**
- `@ProductBuilder I want a "Forgot password" feature` — the agent searches for duplicates, creates a GitHub issue, and responds with a link
- `@ProductBuilder please create an implementation plan` (in a thread with a linked issue) — adds a `/opencode` comment to trigger the AI coding agent
- `@ProductBuilder where do we stand?` (in a thread) — checks issue/PR status and summarizes progress

Then manually:

1. Install the OpenCode GitHub App on the repo: https://github.com/apps/opencode-agent
2. Review and customize the scaffolded files in the target repo:
   - `.productbuilding/preview/config.yml` — preview contract (ports, healthcheck, migrations)
   - `.productbuilding/preview/docker-compose.yml` — preview Compose file (services, Dockerfiles)
   - `AGENTS.md` — project conventions for OpenCode
3. Commit and push the scaffolded files in the target repo
4. Commit and push the updated `secrets/*.enc` files in this repo

---

## Ongoing Operations

### Stop/start instance (cost savings)

```bash
mise run instance-stop
mise run instance-start
```

### SSH into instance

```bash
mise run ssh
mise run ssh -- "docker ps"    # Run a command directly
```

### Re-provision instance

If the instance is corrupt or you want a fresh start:

```bash
# Taint the instance to force recreation
cd infrastructure-mgmt/main
eval "$(.mise/lib/load-secrets)"
tofu taint aws_instance.orchestrator
tofu apply

# The new instance will:
# 1. Boot with cloud-init (installs Docker, creates preview-net)
# 2. After orchestrator is deployed: reconciliation rebuilds all previews
```

The Elastic IP remains stable, so DNS continues to work.

### Add a new target repo

```bash
mise run onboard-target     # Registers target + scaffolds files in target repo
mise run secrets-encrypt    # Re-encrypt updated targets.auto.tfvars
mise run infra-apply        # Creates webhook, Secrets Manager entry, GitHub Actions secret
mise run deploy             # CRITICAL: Redeploy orchestrator to load new target config
# Then: install OpenCode GitHub App, customize scaffolded files, commit + push
# Don't forget to commit the updated secrets/*.enc files
```

### View preview logs

Access application logs from any preview deployment:

```bash
# Get last 100 lines of logs
mise run preview-logs luminor-project myrepo 5

# Get last 500 lines
mise run preview-logs luminor-project myrepo 5 500

# Stream logs in real-time (follow mode)
mise run preview-logs luminor-project myrepo 5 100 true
```

Or use the API directly:
```bash
curl "https://api.productbuilder.luminor-tech.net/previews/owner/repo/pr/logs?tail=100"
```

**Note:** A "View Logs" link is automatically included in all PR comments when previews are ready.

### Preview Contract Options

Target repos can customize their preview deployment via `.productbuilding/preview/config.yml`:

**Database Migrations:**
```yaml
database:
  migrate_command: /app/migrate up
```

**Post-Deploy Commands** (run after preview is healthy):
```yaml
post_deploy_commands:
  - service: app
    command: /app/seed-data
    description: Seed demo data
```

**User-Facing Notes** (shown in PR comments):
```yaml
user_facing_note: "Test login: admin / secret"
```

**Logging Configuration** (for non-Docker stdout logging):
```yaml
logging:
  service: app
  type: file
  path: /var/log/app/*.log
```

**Important:** The orchestrator caches target configuration at startup. After adding a new target, you **must** run `mise run deploy` to restart the orchestrator with the updated configuration. Without this step, webhooks from the new repo will be rejected as "unknown repository".

---

## Troubleshooting

### Webhook rejected as "unknown repository"

The orchestrator loads `targets.json` at startup. If you added a new target but didn't redeploy, the orchestrator doesn't know about it.

**Fix:** `mise run deploy` to restart the orchestrator with updated config.

### Preview stuck or not deploying

```bash
# Check running containers on the instance
mise run ssh -- "docker ps"

# Check preview logs
mise run preview-logs <owner> <repo> <pr>

# Inspect the compose workspace
mise run ssh -- "ls /opt/orchestrator/workspaces/<owner>/<repo>/pr-<number>/"
```

Common causes: Dockerfile build failure, port conflict, missing environment variables in the target repo's compose file.

### Health check failing

The orchestrator polls `https://pr-{number}-preview.{domain}{healthcheck_path}` until it gets a 200 response or the `startup_timeout_seconds` expires.

**Check:**
- `healthcheck_path` in the target's `config.yml` actually returns 200 when healthy
- `internal_port` matches the port the app listens on inside the container
- The app starts within the configured timeout (default 300s)

### Slack notifications not arriving

1. **Channel naming** — channels must be named `#productbuilding-{repo-name}` (the system matches channels to repos by this convention)
2. **Bot token scopes** — the Slack app needs: `chat:write`, `chat:write.public`, `reactions:write`, `reactions:read`, `channels:read`, `channels:history`, `app_mentions:read`, `users:read`
3. **Event subscription** — verify the Request URL is `https://api.{domain}/slack/events` and `app_mention` is subscribed
4. **App reinstalled** — after changing scopes or events, reinstall the app to the workspace

### Preview URL not resolving

DNS delegation may not be propagated yet, or the Elastic IP changed.

```bash
# Verify DNS delegation
dig NS productbuilder.luminor-tech.net +short

# Verify wildcard resolution points to Elastic IP
dig A anything.productbuilder.luminor-tech.net +short

# Compare with the actual Elastic IP
mise run infra-output | grep elastic_ip
```

### Re-provisioning after instance corruption

```bash
cd infrastructure-mgmt/main
eval "$(.mise/lib/load-secrets)"
tofu taint aws_instance.orchestrator
tofu apply
# New instance boots with cloud-init, then:
mise run deploy
# Reconciliation rebuilds active previews from GitHub state
```

The Elastic IP stays stable, so DNS continues to work.
