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

### Step 1b: Configure Slash Commands (Optional but Recommended)

Slash commands provide a more convenient alternative to @mentions:

1. In your Slack app, go to **Slash Commands** → **Create New Command**
2. Add these three commands:

| Command | Request URL | Short Description | Usage Hint |
|---------|-------------|-------------------|------------|
| `/create-issue` | `https://api.{your-domain}/slack/commands` | Create a GitHub issue | `<title>` |
| `/create-plan` | `https://api.{your-domain}/slack/commands` | Request implementation plan | `[instructions]` |
| `/implement` | `https://api.{your-domain}/slack/commands` | Request implementation | `[instructions]` |

3. **Important**: Use the same Request URL `https://api.{your-domain}/slack/commands` for all three commands
4. Save each command and reinstall the app to the workspace

**Slash command usage examples:**
- In channel: `/create-issue Fix login bug` → Creates GitHub issue
- In thread: `/create-plan add e2e tests` → Posts `/opencode Please write an implementation plan for this. add e2e tests` to GitHub
- In thread: `/implement optimize for mobile` → Posts `/opencode Please implement the plan. optimize for mobile` to GitHub

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

The bot supports two interaction flows: **@mentions** (traditional) and **slash commands** (new, more convenient).

### Flow 1: Slash Commands (Recommended)

Slash commands provide a cleaner interface than @mentions and are easier to discover.

| Command | Context | Action | Example |
|---------|---------|--------|---------|
| `/create-issue <title>` | Channel | Creates new GitHub issue | `/create-issue Fix login bug` |
| `/create-plan [text]` | Thread only | Posts `/opencode Please write an implementation plan for this. [text]` to GitHub | `/create-plan add e2e tests` |
| `/implement [text]` | Thread only | Posts `/opencode Please implement the plan. [text]` to GitHub | `/implement optimize for mobile` |

**Key behaviors:**
- Slash commands work alongside @mentions — both remain functional
- `/create-plan` and `/implement` **must** be used in a tracked thread (they need to know which issue/PR to post to)
- Additional text after the command is optional but recommended for context
- Both slash commands and @mentions post to GitHub with the `<!-- via-slack -->` marker to prevent echo loops

### Flow 2: @mentions (Traditional)

The original @mention flows are still fully supported:

#### 2a. In-thread @mention → GitHub comment

When a user @mentions the bot **inside a tracked thread** (one created by the bot for an Issue or PR), the message is forwarded as a GitHub comment.

```
Slack thread (tracked)                GitHub Issue/PR
  @ProductBuilder fix alignment  -->  Comment: "Alice via Slack: fix alignment"
  (plain reply without @mention) -->  (ignored, stays in Slack only)
```

- Only `@ProductBuilder` mentions in **tracked threads** are forwarded.
- Plain thread replies without a bot mention are never forwarded — the team can discuss freely in Slack.
- The bot mention (`<@UBOTID>`) is stripped from the text before posting to GitHub.
- The GitHub comment includes attribution (`**DisplayName** [via Slack](link):`) where the link deep-links back to the exact Slack message.
- A `<!-- via-slack -->` marker prevents the resulting GitHub webhook from echoing back to Slack (loop prevention).

**Which GitHub number is used?** The thread's `slack_threads` record tracks both `github_issue_id` and `github_pr_id`. When both are set (PR phase), the comment is posted on the PR. Otherwise it goes to the Issue.

### 2. Top-level @mention → New GitHub issue

When a user @mentions the bot **directly in the channel** (not in a thread), a new GitHub issue is created in the linked repository.

```
Slack channel #productbuilding-webapp
  @ProductBuilder Add dark mode    -->  New Issue: "Add dark mode"
                                        Body: "Requested by Alice via Slack"
```

- The channel must follow the naming convention `#productbuilding-{repo-name}` — the system resolves the channel ID to its name via the Slack API, then extracts the repo name.
- The issue title is the message text (with the bot mention stripped).
- The issue body includes who requested it (Slack display name, no `@` prefix) and a deep link back to the Slack message.
- The `<!-- via-slack -->` marker is included to prevent echo loops.
- @mentions in channels that don't match the naming convention are silently ignored.

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
A: Yes, two ways:
1. **Slash commands (easier)**: Use `/create-plan` or `/implement` in a tracked thread
2. **@mentions**: @mention the ProductBuilder bot in a tracked thread and the message (minus the mention) is posted as a GitHub comment with your Slack display name and a deep link back to the Slack message. Plain thread replies are not forwarded.

**Q: Can I create issues from Slack?**
A: Yes, two ways:
1. **Slash command (easier)**: `/create-issue Add a login page`
2. **@mention**: @mention the bot in the channel (not in a thread): `@ProductBuilder Add a login page`. The message text becomes the issue title, and the body includes your name and a link back to the Slack message.

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
