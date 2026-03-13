# Slack-GitHub-Orchestrator Integration Guide

## Overview

The ProductBuilder orchestrator integrates with **Slack** to mirror GitHub activity in real-time. Every Issue, Pull Request, and comment gets its own Slack thread for team communication.

### Architecture Principles

1. **GitHub is the Source of Truth** - Slack is a "thin facade". All actions originate from GitHub.
2. **Bi-Directional When Requested** - @mentioning the bot in a tracked thread forwards the message to GitHub as a comment. @mentioning the bot in the channel (top-level) creates a new GitHub issue. Plain thread replies are ignored.
3. **One Channel Per Repository** - Channel must be named `#productbuilding-{repo-name}` (this naming convention is enforced by the system for issue creation)
4. **One Thread Per Issue/PR** - All activity (comments, status updates) posts to the same thread
5. **Non-Blocking** - Slack failures log warnings only, never block the main orchestrator flow
6. **Debounced Updates** - Rapid changes are batched (2-second window) to avoid spam

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
   - `channels:read` - Resolve channel names (required for issue creation from @mentions)
   - `app_mentions:read` - Receive @mention events (for Slack-to-GitHub bridge)
   - `users:read` - Resolve user display names (for GitHub comment attribution)
7. Navigate to **Event Subscriptions**:
   - Enable Events: **On**
   - Request URL: `https://api.{your-domain}/slack/events` (Slack will send a challenge — the orchestrator responds automatically)
   - Under **Subscribe to bot events**, add: `app_mention`
   - Click **Save Changes**
8. Navigate to **Basic Information** and copy the **Signing Secret**

   ⚠️ **Save this signing secret securely** - you'll need it as the `SLACK_SIGNING_SECRET` env var
9. Navigate back to **OAuth & Permissions**
10. Click **Install to Workspace** (or **Reinstall** if updating an existing app)
11. Copy the **Bot User OAuth Token** (starts with `xoxb-`)

   ⚠️ **Save this token securely** - you'll need it in Step 4

### Step 1b: Configure Slash Command (Optional but Recommended)

The `/create-issue` slash command provides a quick way to create GitHub issues from Slack channels:

1. In your Slack app, go to **Slash Commands** → **Create New Command**
2. Add this command:

| Command | Request URL | Short Description | Usage Hint |
|---------|-------------|-------------------|------------|
| `/create-issue` | `https://api.{your-domain}/slack/commands` | Create a GitHub issue | `<title>` |

3. Save and reinstall the app to the workspace

**Slash command usage:**
- In channel: `/create-issue Fix login bug` → Creates GitHub issue in the configured repository

### Step 1c: Configure Message Shortcuts (Recommended for Thread Actions)

Message shortcuts work in threads and provide a better UX than slash commands for issue/PR actions:

1. In your Slack app, go to **Interactivity & Shortcuts**
2. Enable **Interactivity** and set Request URL: `https://api.{your-domain}/slack/interactions`
3. Under **Shortcuts**, click **Create New Shortcut** and select **On messages**
4. Create these three message shortcuts:

| Name | Short Description | Callback ID |
|------|-------------------|-------------|
| Create implementation plan | Request OpenCode to write an implementation plan | `create_plan` |
| Implement this | Request OpenCode to implement the plan | `implement` |
| Add comment | Add a comment to this GitHub issue/PR | `add_comment` |

5. Save changes and reinstall the app to the workspace

**Message shortcut usage:**
1. In a ProductBuilder thread, click the three-dot menu (More actions) on any ProductBuilder message
2. Select the desired action:
   - **Create implementation plan** - Posts `/opencode Please write an implementation plan for this.` to GitHub
   - **Implement this** - Posts `/opencode Please implement the plan.` to GitHub
   - **Add comment** - Opens a modal dialog where you can type and submit a comment to GitHub

**Note:** Message shortcuts only work on ProductBuilder-created messages in tracked threads.

---

### Step 2: Create Slack Channels

**Manual Step Required**

For each repository you want to integrate:

1. In Slack, create a new channel: `#productbuilding-{repo-name}`
   - Example: For `mycompany/webapp`, create `#productbuilding-webapp`
   - **Important**: The channel name **must** follow this exact pattern. The system uses the naming convention `productbuilding-{repo_name}` to match channels to repositories (e.g. for creating issues from @mentions). The `{repo-name}` part must match the `repo_name` field in the target config exactly.
2. Invite the ProductBuilder bot to the channel:
   - Type: `/invite @ProductBuilder Bot`

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

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `SLACK_SIGNING_SECRET` | For Slack-to-GitHub bridge | Signing secret from Slack app's Basic Information page. Used to verify incoming Events API requests. If empty, the `/slack/events` endpoint rejects all requests. |
| `SLACK_WORKSPACE` | For deep links | Slack workspace subdomain (e.g. `luminor-tech` for `luminor-tech.slack.com`). Used to build deep links from GitHub comments back to the originating Slack messages. If empty, links default to `slack.com`. |

Set these on the orchestrator host (e.g. in the `.env` file consumed by Docker Compose, or in cloud-init).

---

## Bi-Directional Flow: Slack-to-GitHub

The bot supports three interaction flows that work together:

### Flow 1: Message Shortcuts (Recommended for Threads)

Message shortcuts provide the best UX for thread-based actions. Access them via the three-dot menu (More actions) on any ProductBuilder message.

| Shortcut | Where | Action |
|----------|-------|--------|
| **Create implementation plan** | ProductBuilder message in thread | Posts `/opencode Please write an implementation plan for this.` to GitHub |
| **Implement this** | ProductBuilder message in thread | Posts `/opencode Please implement the plan.` to GitHub |
| **Add comment** | ProductBuilder message in thread | Opens modal → User types comment → Posts to GitHub with attribution |

**Key behaviors:**
- Shortcuts **must** be used on ProductBuilder-created messages in tracked threads
- The shortcut handler looks up the thread to determine which GitHub issue/PR to post to
- Comments posted via the "Add comment" shortcut include user attribution and a deep link back to Slack
- All posts include the `<!-- via-slack -->` marker to prevent echo loops

### Flow 2: Slash Command

The `/create-issue` slash command provides quick issue creation from channels:

| Command | Where | Action | Example |
|---------|-------|--------|---------|
| `/create-issue <title>` | Channel | Creates new GitHub issue | `/create-issue Fix login bug` |

**Key behaviors:**
- Works alongside message shortcuts and @mentions
- The channel must follow the naming convention `#productbuilding-{repo-name}`
- Creates issue with attribution and Slack deep link

### Flow 3: @mentions (Traditional)

The original @mention flows are still fully supported:

#### 3a. In-thread @mention → GitHub comment

When a user @mentions the bot **inside a tracked thread**, the message is forwarded as a GitHub comment.

```
Slack thread (tracked)                GitHub Issue/PR
  @ProductBuilder fix alignment  -->  Comment: "Alice via Slack: fix alignment"
  (plain reply without @mention) -->  (ignored, stays in Slack only)
```

- Only `@ProductBuilder` mentions in **tracked threads** are forwarded.
- Plain thread replies without a bot mention are never forwarded.
- The bot mention is stripped from the text before posting.
- The GitHub comment includes attribution with a deep link back to Slack.
- A `<!-- via-slack -->` marker prevents echo loops.

**Which GitHub number is used?** When both issue and PR IDs are tracked (PR phase), the comment is posted on the PR. Otherwise it goes to the Issue.

#### 3b. Top-level @mention → New GitHub issue

When a user @mentions the bot **directly in the channel** (not in a thread), a new GitHub issue is created.

```
Slack channel #productbuilding-webapp
  @ProductBuilder Add dark mode    -->  New Issue: "Add dark mode"
                                         Body: "Requested by Alice via Slack"
```

- The channel must follow the naming convention `#productbuilding-{repo-name}`.
- The issue title is the message text (with the bot mention stripped).
- The issue body includes attribution and a deep link.
- The `<!-- via-slack -->` marker prevents echo loops.

---

## Configuration Reference

### TargetConfig Fields

| Field | Required | Description |
|-------|----------|-------------|
| `repo_owner` | ✅ | GitHub organization or user |
| `repo_name` | ✅ | Repository name |
| `github_pat` | ✅ | GitHub Personal Access Token |
| `webhook_secret` | ✅ | Secret from GitHub webhook setup |
| `slack_channel` | ❌ | Slack channel name for outbound notifications (omit to disable Slack). Must match the channel naming convention `#productbuilding-{repo_name}`. |
| `slack_bot_token` | ❌ | Bot token (omit to disable Slack). Also used for resolving channel names and user display names. |

**Note**: If `slack_channel` or `slack_bot_token` is empty/omitted, Slack integration is disabled for that repo (silently, no errors). Inbound @mentions (issue creation) rely on the channel naming convention, not this field.

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

2. **Use least-privilege** - GitHub PAT should be fine-grained with minimal permissions:
   - `Contents: Read` (clone the repo)
   - `Issues: Read+Write` (create issues, post comments)
   - `Pull requests: Read+Write` (post preview URL comments)
   - `Metadata: Read` (required by default)

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

**Q: Can I post comments from Slack to GitHub?**
A: Yes, three ways:
1. **Message shortcuts (recommended)**: Click "More actions" on a ProductBuilder message and select "Add comment" to open a modal dialog
2. **Request OpenCode actions**: Use "Create implementation plan" or "Implement this" shortcuts to trigger `/opencode` commands
3. **@mentions**: @mention the ProductBuilder bot in a tracked thread and the message (minus the mention) is posted as a GitHub comment

**Q: Can I create issues from Slack?**
A: Yes, two ways:
1. **Slash command (easiest)**: `/create-issue Add a login page`
2. **@mention**: @mention the bot in the channel (not in a thread): `@ProductBuilder Add a login page`

**Q: Will Slack-to-GitHub comments echo back to Slack?**
A: No. Comments and issues posted from Slack include a `<!-- via-slack -->` marker. The GitHub webhook handler detects this marker and skips the Slack notification, preventing loops.

**Q: What if I recreate a Slack channel (same name, new ID)?**
A: The system resolves channel IDs to names via the Slack API at runtime, so a recreated channel with the same name works automatically. No config changes needed.

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
- [ ] Create test Issue via `/create-issue` → verify Slack notification
- [ ] Create test PR → verify thread created
- [ ] Use `/create-plan` in thread → verify `/opencode` comment posted to GitHub
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
