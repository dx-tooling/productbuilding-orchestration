# Runbook — Setup from Scratch

This documents the full procedure to provision the orchestration infrastructure from zero. Follow these steps whenever you need to set up a fresh environment (new AWS account, disaster recovery, etc.).

## Prerequisites

On your local machine:

- [mise](https://mise.jdx.dev/) installed
- [OpenTofu](https://opentofu.org/) installed (`brew install opentofu`)
- [AWS CLI](https://aws.amazon.com/cli/) installed
- SSH key pair (default: `~/.ssh/id_rsa.pub`)

You also need:

- An AWS account with API credentials (access key + secret key)
- A GitHub PAT with org-level management permissions (for creating webhooks and repo secrets)
- Access to the DNS management panel for `luminor-tech.net`

## Step 1: Populate secrets.yaml

Copy and fill in `secrets.sample.yaml`:

```bash
cp secrets.sample.yaml secrets.yaml
# Edit secrets.yaml with your AWS credentials and GitHub management PAT
```

## Step 2: Bootstrap (one-time per AWS account)

This creates the S3 bucket and DynamoDB table for OpenTofu state management.

```bash
cd infrastructure-mgmt/bootstrap
export AWS_ACCESS_KEY_ID=<from secrets.yaml>
export AWS_SECRET_ACCESS_KEY=<from secrets.yaml>
tofu init
tofu apply
```

Note the outputs — the S3 bucket name and DynamoDB table name are hardcoded in `infrastructure-mgmt/main/backend.tf`. If you're starting in a new AWS account, update `backend.tf` with the new bucket name.

## Step 3: Configure main project variables

Create `infrastructure-mgmt/main/terraform.tfvars` (gitignored):

```hcl
github_mgmt_pat = "github_pat_..."
ssh_public_key  = "ssh-rsa AAAA..."
```

## Step 4: Apply main infrastructure

```bash
mise run infra-apply
```

This creates: Route53 hosted zone, wildcard DNS record, Elastic IP, EC2 instance, security groups, IAM role, and SSH key pair.

Note the outputs, especially `route53_nameservers`.

## Step 5: DNS delegation (manual)

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

## Step 6: Verify instance is healthy

```bash
mise run instance-status    # Should show "running"
mise run ssh -- "docker --version && docker network ls | grep preview-net"
```

## Step 7: Onboard target repos

For each target repository:

```bash
mise run onboard-target     # Interactive — prompts for repo details + credentials
mise run infra-apply        # Creates webhook, Secrets Manager entry, GitHub Actions secret
```

Then manually:

1. Install the Claude GitHub App on the repo: https://github.com/apps/claude
2. Add `.github/workflows/claude.yml` to the repo
3. Add `CLAUDE.md` to the repo
4. Add `preview.yml` + `docker-compose.preview.yml` to the repo

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
mise run onboard-target
mise run infra-apply
# Then: install Claude GitHub App, add workflow + preview contract to repo
```
