# Slack Integration - Quick Start

This directory contains comprehensive documentation for integrating Slack with the ProductBuilder orchestrator.

## 📚 Documentation Files

| Document | Purpose | Audience |
|----------|---------|----------|
| **[SLACK_INTEGRATION.md](./SLACK_INTEGRATION.md)** | Complete setup guide | DevOps, Team Leads |
| **[SLACK_VERIFICATION.md](./SLACK_VERIFICATION.md)** | Testing checklist & troubleshooting | Developers, QA |

## 🚀 Quick Start (5 minutes)

### 1. Create Slack Bot
- Go to https://api.slack.com/apps
- Create app, add scopes: `chat:write`, `chat:write.public`, `reactions:write`, `app_mentions:read`, `users:read`
- Enable Event Subscriptions: subscribe to `app_mention`, set Request URL to `https://api.{domain}/slack/events`
- Copy **Signing Secret** from Basic Information (set as `SLACK_SIGNING_SECRET` env var)
- Install to workspace, copy bot token (starts with `xoxb-`)

### 2. Create Channel
- In Slack: `#productbuilding-{your-repo-name}`
- Invite bot: `/invite @ProductBuilder Bot`

### 3. Run Onboarding
```bash
mise run onboard-target
```
- Choose "y" when asked to enable Slack integration
- Paste your bot token
- Confirm channel name

### 4. Deploy
```bash
mise run secrets-encrypt
mise run infra-apply
```

### 5. Test
- Create a test Issue in your repo
- Check Slack - you should see a notification!

## 🔗 Integration Architecture

```
GitHub Webhook → Orchestrator → Slack API
     ↓                ↓              ↓
   Issue/PR      Notifier       Channel Thread
   Comment       Debouncer      Emoji Reactions

Slack @mention → Orchestrator → GitHub API
     ↓                ↓              ↓
  app_mention    Event Handler   Issue/PR Comment
  (in thread)    Signature Check  (with attribution)
```

**Key Features**:
- **Bi-Directional**: GitHub → Slack (automatic), Slack → GitHub (via @mention)
- **One Channel Per Repo**: Manual setup for control
- **One Thread Per Issue/PR**: All activity grouped
- **Debounced Updates**: 2-second window prevents spam
- **Non-Blocking**: Slack failures don't affect orchestrator
- **Emoji Status**: 🔄 Building → ✅ Ready → ❌ Failed
- **Loop Prevention**: Comments from Slack include a marker to prevent echo

## ⚙️ Configuration

Add to your `targets.json`:

```json
{
  "repo_owner": "mycompany",
  "repo_name": "webapp",
  "github_pat": "ghp_...",
  "webhook_secret": "...",
  "slack_channel": "#productbuilding-webapp",
  "slack_bot_token": "xoxb-..."
}
```

**Note**: Both `slack_channel` and `slack_bot_token` are optional. Omit either to disable Slack for that repo.

## 🧪 Testing

Follow the comprehensive test suite in [SLACK_VERIFICATION.md](./SLACK_VERIFICATION.md), including:
- Issue/PR creation notifications
- Threaded comment replies
- Status emoji reactions (🔄✅❌🎉)
- Debouncing behavior
- Closure handling

## 🐛 Common Issues

| Issue | Solution |
|-------|----------|
| No notifications | Check bot is invited to channel |
| Duplicate threads | Run database migrations |
| Missing emoji reactions | Add `reactions:write` scope |
| Webhook errors | Verify secret in targets.json |
| @mention not forwarding | Check `SLACK_SIGNING_SECRET` env var and `app_mentions:read` scope |

See [SLACK_VERIFICATION.md](./SLACK_VERIFICATION.md) for full troubleshooting guide.

## 📋 Team Onboarding Checklist

```markdown
## Week 1: Setup
- [ ] Slack channel created
- [ ] Bot invited to channel
- [ ] GitHub webhook verified
- [ ] Test Issue created

## Week 2: Team Training
- [ ] Share SLACK_INTEGRATION.md with team
- [ ] Establish conventions (Slack vs GitHub comments)
- [ ] Pin important threads

## Month 1: Review
- [ ] Check noise levels
- [ ] Adjust preferences
- [ ] Document custom workflows
```

## 🔐 Security

- **Rotate tokens**: GitHub PAT every 90 days, Slack token every 180 days
- **Least privilege**: Use minimal bot scopes
- **Secure storage**: Use AWS Secrets Manager (never commit tokens)
- **Private channels**: Consider for sensitive repos

## 📞 Support

1. Check [SLACK_VERIFICATION.md](./SLACK_VERIFICATION.md) troubleshooting
2. Review orchestrator logs: `journalctl -u orchestrator -f`
3. File issue in `luminor-productbuilding-orchestration` repo

---

## Related Documentation

- [Main Orchestrator README](../README.md)
- [Deployment Guide](./DEPLOYMENT.md)
- [Architecture Overview](./ARCHITECTURE.md)
- [GitHub Webhook Security](./GITHUB_WEBHOOKS.md)

---

*Last updated: March 2026*
