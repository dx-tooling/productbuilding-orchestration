# Slack-GitHub-Orchestrator Integration Guide

## Overview

The ProductBuilder orchestrator integrates with **Slack** to mirror GitHub activity in real-time. Every Issue, Pull Request, and comment gets its own Slack thread for team communication.

### Architecture Principles

1. **GitHub is the Source of Truth** - Slack is a "thin facade". All actions originate from GitHub.
2. **One Channel Per Repository** - Manual setup: `#productbuilding-{repo-name}`
3. **One Thread Per Issue/PR** - All activity (comments, status updates) posts to the same thread
4. **Non-Blocking** - Slack failures log warnings only, never block the main orchestrator flow
5. **Debounced Updates** - Rapid changes are batched (2-second window) to avoid spam

---

## Prerequisites

Before you begin, ensure you have:

- [ ] AWS account with appropriate permissions (for Secrets Manager)
- [ ] GitHub organization admin access (to configure webhooks)
- [ ] Slack workspace admin access (to create channels and install bot)
- [ ] Terraform installed (for infrastructure provisioning)
- [ ] Access to the orchestrator deployment environment

---

## Step-by-Step Setup

### Step 1: Create Slack Bot

1. Go to [Slack API Apps](https://api.slack.com/apps)
2. Click **Create New App** → **From scratch**
3. Name it: `ProductBuilder Bot`
4. Select your workspace
5. Navigate to **OAuth & Permissions**
6. Add the following **Bot Token Scopes**:
   - `chat:write` - Post messages to channels
   - `chat:write.public` - Post to public channels
   - `reactions:write` - Add emoji reactions
   - `channels:read` - Read channel information
7. Click **Install to Workspace**
8. Copy the **Bot User OAuth Token** (starts with `xoxb-`)
   
   ⚠️ **Save this token securely** - you'll need it in Step 4

---

### Step 2: Create Slack Channels

**Manual Step Required**

For each repository you want to integrate:

1. In Slack, create a new channel: `#productbuilding-{repo-name}`
   - Example: For `mycompany/webapp`, create `#productbuilding-webapp`
2. Invite the ProductBuilder bot to the channel:
   - Type: `/invite @ProductBuilder Bot`
3. Note the exact channel name (case-sensitive)

**Why manual?** We intentionally don't auto-create channels to respect your Slack workspace organization and naming conventions.

---

### Step 3: Configure GitHub Webhooks

For each repository:

1. Go to **Settings** → **Webhooks** → **Add webhook**
2. **Payload URL**: `https://orchestrator.{your-domain}/webhook`
3. **Content type**: `application/json`
4. **Secret**: Generate a strong random secret (save for Step 4)
5. **Events to subscribe to**:
   - ✅ Pull requests
   - ✅ Issues  
   - ✅ Issue comments
6. Click **Add webhook**

**Verify webhook is active** - You should see a green checkmark next to the webhook after the first ping.

---

### Step 4: Configure Orchestrator Secrets

Add the following to AWS Secrets Manager (or your secrets store):

**Target Configuration JSON** (`targets.json`):

```json
[
  {
    "repo_owner": "mycompany",
    "repo_name": "webapp",
    "github_pat": "ghp_xxxxxxxxxxxxxxxxxxxx",
    "webhook_secret": "your-webhook-secret-from-step-3",
    "slack_channel": "#productbuilding-webapp",
    "slack_bot_token": "xoxb-your-bot-token-from-step-1"
  }
]
```

**Terraform Example**:

```hcl
# secrets.tf
resource "aws_secretsmanager_secret" "targets_config" {
  name        = "${local.name_prefix}-targets-config"
  description = "ProductBuilder target repositories configuration"
}

resource "aws_secretsmanager_secret_version" "targets_config" {
  secret_id = aws_secretsmanager_secret.targets_config.id
  secret_string = jsonencode([
    {
      repo_owner      = "mycompany"
      repo_name       = "webapp"
      github_pat      = var.github_pat
      webhook_secret  = var.webhook_secret
      slack_channel   = "#productbuilding-webapp"
      slack_bot_token = var.slack_bot_token
    }
  ])
}
```

---

### Step 5: Deploy Orchestrator

Ensure your orchestrator deployment:

1. Has the `targets.json` mounted or accessible
2. Has network access to:
   - GitHub API (`api.github.com`)
   - Slack API (`slack.com`)
3. Database migrations have run (includes `slack_threads` table)

**Verify migrations ran**:
```bash
sqlite3 /path/to/orchestrator.db ".tables"
# Should show: previews, slack_threads
```

---

### Step 6: Verify Integration

#### Test 1: Issue Creation
1. Create a new Issue in your integrated repo
2. Check Slack - you should see a message in the channel with 📝 icon
3. Verify the thread is created

#### Test 2: PR Creation
1. Create a Pull Request
2. Check Slack - you should see a message with 🔀 icon
3. Comment on the PR
4. Verify comment appears in the thread

#### Test 3: Emoji Reactions
1. Watch for these emoji reactions on threads:
   - 🔄 - Build in progress
   - ✅ - Preview ready
   - ❌ - Build failed
   - 🎉 - PR merged

#### Test 4: Multiple Rapid Updates
1. Make several rapid commits to a PR
2. Verify Slack doesn't spam (messages should be debounced)
3. Only the latest status should appear

---

## Configuration Reference

### TargetConfig Fields

| Field | Required | Description |
|-------|----------|-------------|
| `repo_owner` | ✅ | GitHub organization or user |
| `repo_name` | ✅ | Repository name |
| `github_pat` | ✅ | GitHub Personal Access Token |
| `webhook_secret` | ✅ | Secret from GitHub webhook setup |
| `slack_channel` | ❌ | Slack channel name (omit to disable Slack) |
| `slack_bot_token` | ❌ | Bot token (omit to disable Slack) |

**Note**: If `slack_channel` or `slack_bot_token` is empty/omitted, Slack integration is disabled for that repo (silently, no errors).

---

## Troubleshooting

### Issue: No Slack notifications appearing

**Checklist**:
1. ✅ Bot is invited to the channel (`/invite @ProductBuilder Bot`)
2. ✅ `targets.json` has correct `slack_channel` name (case-sensitive)
3. ✅ `slack_bot_token` is valid (not expired)
4. ✅ GitHub webhook is delivering events (check webhook "Recent Deliveries")
5. ✅ Orchestrator logs show no errors: `journalctl -u orchestrator -f`

**Debug Logs**:
```bash
# Check orchestrator logs for Slack activity
journalctl -u orchestrator -f | grep -i slack
```

### Issue: Duplicate threads created

**Cause**: Database migration not run or `slack_threads` table missing.

**Fix**:
```bash
# Run migrations
./orchestrator migrate

# Or manually create table (see migrations/002_create_slack_threads.up.sql)
```

### Issue: Emoji reactions not appearing

**Check**:
- Bot has `reactions:write` scope
- Thread was successfully created (check parent message exists)

### Issue: Webhook signature errors

**Check**:
- `webhook_secret` in `targets.json` matches GitHub webhook secret
- Secret has no extra whitespace or quotes

---

## Security Best Practices

1. **Rotate tokens regularly** - Set calendar reminders to rotate:
   - GitHub PAT: every 90 days
   - Slack bot token: every 180 days

2. **Use least-privilege** - GitHub PAT should have minimal scopes:
   - `repo` (for private repos)
   - `read:org` (if using org-level webhooks)

3. **Secure secrets storage** - Never commit tokens to git:
   - ✅ AWS Secrets Manager
   - ✅ HashiCorp Vault
   - ✅ 1Password/Bitwarden Secrets Automation
   - ❌ Never: hardcoded in source, env files in repo

4. **Channel naming** - Consider making channels private if:
   - Repo contains sensitive code
   - External contractors have Slack access

---

## Advanced Configuration

### Custom Emoji Mapping

You can customize status emoji by modifying `formatEventMessage()` in:
`internal/slack/domain/notifier.go`

Default mapping:
- PR opened: (no emoji, just message)
- Build in progress: 🔄
- Preview ready: ✅
- Build failed: ❌
- PR merged: 🎉
- Issue/PR closed: ✅

### Adjusting Debounce Window

Default: 2 seconds

To change, modify in `internal/slack/domain/notifier.go`:
```go
n.debouncer.Debounce(key, 2*time.Second, func() { ... })
```

---

## FAQ

**Q: Can I use different channels for Issues vs PRs?**  
A: Not currently. One channel per repo handles both.

**Q: Can I thread comments from PR reviews?**  
A: Currently only handles `issue_comment` events (which includes PR comments). Review comments are not yet supported.

**Q: What happens if Slack is down?**  
A: Orchestrator continues working. Slack failures are logged as warnings but never block preview deployments.

**Q: Can I disable Slack for specific repos?**  
A: Yes - simply omit `slack_channel` and `slack_bot_token` from that repo's target config.

**Q: How do I add a new repo to existing Slack integration?**  
A: Follow Steps 2-4 for the new repo. No need to create a new bot.

---

## Onboarding Checklist

Give this checklist to new teams:

```markdown
## Slack Integration Onboarding

### Week 1: Setup
- [ ] Create Slack channel `#productbuilding-{repo-name}`
- [ ] Invite ProductBuilder bot to channel
- [ ] Verify GitHub webhook is configured (ask admin)
- [ ] Confirm repo is in `targets.json` with Slack credentials

### Week 2: Validation  
- [ ] Create test Issue → verify Slack notification
- [ ] Create test PR → verify thread created
- [ ] Comment on PR → verify appears in thread
- [ ] Check emoji reactions appear on status changes

### Week 3: Team Training
- [ ] Share this guide with team
- [ ] Establish conventions (thread replies vs GitHub comments)
- [ ] Pin important threads in Slack
- [ ] Archive old preview threads (manual cleanup)

### Month 1: Review
- [ ] Check channel noise levels
- [ ] Adjust notification preferences if needed
- [ ] Document any custom workflows
```

---

## Support

For issues or questions:
1. Check this guide's Troubleshooting section
2. Review orchestrator logs
3. File an issue in the `luminor-productbuilding-orchestration` repo
4. Contact: #productbuilder-support (Slack channel)

---

## Related Documentation

- [Deployment Guide](./DEPLOYMENT.md)
- [Architecture Overview](./ARCHITECTURE.md)
- [GitHub Webhook Security](./GITHUB_WEBHOOKS.md)
- [Troubleshooting](./TROUBLESHOOTING.md)

---

*Last updated: March 2026*
