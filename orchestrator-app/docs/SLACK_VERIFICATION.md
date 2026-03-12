# Slack Integration Verification Checklist

Use this checklist to verify your Slack-GitHub-Orchestrator integration is working correctly.

## Pre-Flight Checks

Before testing, confirm these are in place:

- [ ] Slack channel created: `#productbuilding-{repo-name}`
- [ ] ProductBuilder Bot invited to channel (`/invite @ProductBuilder Bot`)
- [ ] GitHub webhook configured with `issues`, `pull_request`, and `issue_comment` events
- [ ] `targets.json` includes `slack_channel` and `slack_bot_token`
- [ ] Orchestrator deployed with latest migrations (includes `slack_threads` table)
- [ ] Bot token has required scopes: `chat:write`, `chat:write.public`, `reactions:write`, `channels:read`, `app_mentions:read`, `users:read`
- [ ] Event Subscriptions enabled with `app_mention` event subscribed
- [ ] `SLACK_SIGNING_SECRET` env var set on the orchestrator host

---

## Test Suite

### Test 1: Issue Creation
**Purpose**: Verify new Issues create Slack threads

**Steps**:
1. Go to your integrated GitHub repo
2. Click **Issues** → **New issue**
3. Enter:
   - Title: "Slack Integration Test - Issue Creation"
   - Body: "This is a test issue to verify Slack integration."
4. Click **Submit new issue**

**Expected Result**:
- [ ] Message appears in Slack channel within 5 seconds
- [ ] Message format: 📝 *Issue* #N: {title}
- [ ] Message includes "View on GitHub" link
- [ ] A thread (not just a message) is created

**Troubleshooting**:
- No message? Check orchestrator logs for "issue webhook received"
- Check webhook "Recent Deliveries" in GitHub repo settings
- Verify `slack_channel` name is exact (case-sensitive)

---

### Test 2: PR Creation
**Purpose**: Verify new PRs create Slack threads

**Steps**:
1. Create a new branch: `git checkout -b test/slack-integration`
2. Make any commit (even a README edit)
3. Push branch: `git push origin test/slack-integration`
4. Open a Pull Request on GitHub
5. Enter:
   - Title: "Slack Integration Test - PR"
   - Description: "Testing Slack thread creation for PRs"

**Expected Result**:
- [ ] Message appears in Slack with 🔀 icon
- [ ] Message format: 🔀 *Pull Request* #N: {title}
- [ ] Thread is created and linked

**Troubleshooting**:
- Check orchestrator logs for "pull_request" webhook events
- Verify `slack_channel` and `slack_bot_token` are set in targets config

---

### Test 3: Threaded Comments
**Purpose**: Verify comments stay in threads

**Steps**:
1. Go to the test Issue created in Test 1
2. Add a comment: "Test comment from GitHub"
3. Check Slack - find the Issue thread

**Expected Result**:
- [ ] Comment appears as reply in the existing thread
- [ ] Format: 💬 @{author}: {truncated comment}
- [ ] "View full comment" link is present

**Steps (PR)**:
1. Go to the test PR from Test 2
2. Add a comment in the "Conversation" tab
3. Check Slack - find the PR thread

**Expected Result**:
- [ ] Same behavior as Issue comments
- [ ] Comments threaded under the PR parent message

---

### Test 4: Status Emoji Reactions
**Purpose**: Verify emoji reactions show status

**Steps**:
1. Create a new PR with code changes (or use Test 2 PR)
2. Ensure the preview deployment triggers
3. Watch the Slack thread for the PR

**Expected Result** (in order):
- [ ] 🔄 appears when build starts
- [ ] 🔄 changes to ✅ when preview is ready
- [ ] Message includes preview URL link

**For a failing build**:
- [ ] 🔄 changes to ❌ when build fails
- [ ] Error details posted to thread

---

### Test 5: PR Closure
**Purpose**: Verify thread closure handling

**Steps**:
1. Close the test PR from Test 2 (without merging)
2. Watch the Slack thread

**Expected Result**:
- [ ] Message posted: "🔒 *Closed* — Preview removed"

**Steps (Merge)**:
1. Create another test PR
2. Merge it
3. Watch the Slack thread

**Expected Result**:
- [ ] Message posted: "🎉 *Merged* — Preview will be removed shortly"
- [ ] 🎉 emoji reaction added to parent message

---

### Test 6: Debouncing
**Purpose**: Verify rapid updates don't spam

**Steps**:
1. Create a PR
2. Make 5 rapid commits to the branch:
   ```bash
   for i in {1..5}; do
     echo "Update $i" >> README.md
     git add README.md
     git commit -m "Update $i"
     git push
     sleep 1
   done
   ```
3. Watch Slack for 10 seconds

**Expected Result**:
- [ ] Only ONE status update message appears
- [ ] Not 5 separate messages
- [ ] The latest status is the one shown

**Technical Detail**: Updates are debounced with a 2-second window.

---

### Test 7: Issue Closure
**Purpose**: Verify issue resolution tracking

**Steps**:
1. Close the test Issue from Test 1
2. Check Slack thread

**Expected Result**:
- [ ] Message posted: "✅ *Closed*"
- [ ] Thread remains for history

---

### Test 8: Slack-to-GitHub Comment (@mention)
**Purpose**: Verify @mentioning the bot in a tracked thread posts a GitHub comment

**Steps**:
1. Find a tracked Slack thread (one created by the bot for an Issue or PR)
2. Reply in the thread with: `@ProductBuilder please fix the alignment`
3. Check the GitHub Issue/PR page

**Expected Result**:
- [ ] A new comment appears on the GitHub Issue/PR
- [ ] Comment format: `**YourDisplayName** [via Slack](link):` followed by the message text
- [ ] The "via Slack" text is a clickable deep link back to the Slack message
- [ ] The bot mention is NOT included in the GitHub comment text
- [ ] The comment contains a `<!-- via-slack -->` HTML marker (view source)
- [ ] The GitHub comment does NOT echo back to Slack (loop prevention)

**Negative Cases**:
- [ ] A plain thread reply (no @mention) does NOT create a GitHub comment
- [ ] An @mention in a non-tracked thread does NOT create a GitHub comment

**Troubleshooting**:
- Check orchestrator logs for "posted github comment from slack" or errors
- Verify `SLACK_SIGNING_SECRET` is set and matches the Slack app's signing secret
- Verify Event Subscriptions are enabled with `app_mention` subscribed
- Verify bot has `app_mentions:read` and `users:read` scopes

---

### Test 9: Issue Creation from Slack (@mention in channel)
**Purpose**: Verify @mentioning the bot in the channel (not in a thread) creates a GitHub issue

**Steps**:
1. Go to the `#productbuilding-{repo-name}` channel
2. Post a message: `@ProductBuilder Add a contact page`
3. Check the GitHub repo's Issues page

**Expected Result**:
- [ ] A new Issue is created on GitHub
- [ ] Issue title: "Add a contact page" (bot mention stripped)
- [ ] Issue body includes: "Requested by YourDisplayName [via Slack](link)"
- [ ] The "via Slack" text is a clickable deep link back to the Slack message
- [ ] Issue body contains `<!-- via-slack -->` marker
- [ ] The resulting GitHub webhook does NOT echo the issue back to Slack as a duplicate

**Negative Cases**:
- [ ] An @mention in a channel that does NOT follow the `#productbuilding-{repo-name}` convention is silently ignored

**Troubleshooting**:
- Check orchestrator logs for "created github issue from slack" or errors
- Verify bot has `channels:read` scope (required to resolve channel ID → name)
- Verify channel name matches the convention exactly: `productbuilding-{repo_name}` where `{repo_name}` matches the target config
- Verify at least one target has a `slack_bot_token` configured

---

### Test 10: Multiple Repos (if applicable)
**Purpose**: Verify isolation between repos

**Steps**:
1. If you have 2+ repos integrated, create Issues in both
2. Check each Slack channel

**Expected Result**:
- [ ] Each repo's channel only shows its own activity
- [ ] No cross-posting between channels

---

## Debugging Commands

### Check Orchestrator Logs
```bash
# View real-time logs
journalctl -u orchestrator -f

# Filter for Slack activity
journalctl -u orchestrator -f | grep -i slack

# Filter for webhook events
journalctl -u orchestrator -f | grep -i webhook
```

### Check GitHub Webhook Deliveries
1. Go to repo → Settings → Webhooks
2. Click on your webhook URL
3. Scroll to "Recent Deliveries"
4. Look for:
   - ✅ Green checkmark = delivered successfully
   - ❌ Red X = delivery failed (check orchestrator is running)

### Verify Database
```bash
# List tables
sqlite3 /path/to/orchestrator.db ".tables"

# Should show: previews, slack_threads

# Check slack_threads table
sqlite3 /path/to/orchestrator.db "SELECT * FROM slack_threads LIMIT 5;"
```

### Test Bot Token
```bash
# Quick API test
curl -H "Authorization: Bearer xoxb-your-token" \
  https://slack.com/api/auth.test

# Should return: {"ok": true, ...}
```

---

## Common Issues & Solutions

### Issue: No notifications at all

**Diagnostic Flow**:
1. Check orchestrator running: `systemctl status orchestrator`
2. Check webhook deliveries in GitHub
3. Check logs for "webhook received" messages
4. Verify `slack_channel` and `slack_bot_token` in targets config
5. Test bot token with curl (see above)

### Issue: Notifications work but no emoji reactions

**Solution**:
- Add `reactions:write` scope to bot
- Reinstall bot to workspace
- Update token in secrets/TFvars

### Issue: Threads not being created (duplicate parent messages)

**Solution**:
- Check `slack_threads` table exists
- Run migrations: `./orchestrator migrate` or restart with auto-migrate
- Check logs for "failed to save slack thread" errors

### Issue: Comments appearing as new messages instead of thread replies

**Solution**:
- This happens when the parent thread wasn't saved to DB
- Check that Issue/PR was created AFTER Slack integration was configured
- Old Issues/PRs won't have threads (only new ones)

### Issue: Bot can't post to channel

**Error in logs**: `channel_not_found` or `not_in_channel`

**Solution**:
1. Verify channel name is correct (case-sensitive)
2. Invite bot to channel: `/invite @ProductBuilder Bot`
3. If private channel, add `groups:write` scope to bot

---

## Sign-Off

Once all tests pass, have team lead sign off:

| Test | Status | Notes |
|------|--------|-------|
| Issue Creation | ✅ / ❌ | |
| PR Creation | ✅ / ❌ | |
| Threaded Comments | ✅ / ❌ | |
| Status Emoji | ✅ / ❌ | |
| PR Closure | ✅ / ❌ | |
| Debouncing | ✅ / ❌ | |
| Issue Closure | ✅ / ❌ | |
| Slack-to-GitHub @mention (thread) | ✅ / ❌ | |
| Slack-to-GitHub issue (@mention in channel) | ✅ / ❌ | |
| Multi-Repo | ✅ / ❌ | (if applicable) |

**Integration Verified By**: _________________  **Date**: _________

**Team**: _________________  **Slack Channel**: _________________

---

## Next Steps After Verification

1. **Team Training**: Walk through this checklist with your team
2. **Establish Conventions**:
   - When to use Slack thread replies vs GitHub comments
   - Which types of issues warrant Slack discussion
3. **Monitor Usage**: Check channel activity after 1 week
4. **Archive Strategy**: Plan for old thread cleanup (manual)

---

## Support

If tests fail after following troubleshooting steps:

1. Collect logs: `journalctl -u orchestrator --since "1 hour ago" > orchestrator.log`
2. File issue at: `luminor-productbuilding-orchestration` repo
3. Include:
   - This checklist with failed tests marked
   - Orchestrator logs (redact tokens)
   - GitHub webhook delivery history screenshot
   - Target config (redact sensitive values)

---

*Document Version: 1.0*
*Last Updated: March 2026*
