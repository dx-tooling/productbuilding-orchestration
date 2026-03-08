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
                            # Registers the target AND scaffolds .productbuilding/, workflow, CLAUDE.md
mise run secrets-encrypt     # Re-encrypt updated targets.auto.tfvars
mise run infra-apply        # Creates webhook, Secrets Manager entry, GitHub Actions secret
```

Then manually:

1. Install the Claude GitHub App on the repo: https://github.com/apps/claude
2. Review and customize the scaffolded files in the target repo:
   - `.productbuilding/preview/config.yml` — preview contract (ports, healthcheck, migrations)
   - `.productbuilding/preview/docker-compose.yml` — preview Compose file (services, Dockerfiles)
   - `CLAUDE.md` — project conventions for Claude Code
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
mise run infra-apply
# Then: install Claude GitHub App, customize scaffolded files, commit + push
# Don't forget to commit the updated secrets/*.enc files
```
