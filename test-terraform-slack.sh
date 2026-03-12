#!/bin/bash
# Test to verify Terraform configuration includes Slack fields in secrets
# This is a regression test for the bug where slack_channel and slack_bot_token
# were not being stored in AWS Secrets Manager.

set -euo pipefail

echo "Testing Terraform Slack configuration..."

# Check that targets.tf includes slack fields in secret version
echo "✓ Checking targets.tf includes Slack fields..."
if grep -q "slack_channel.*=.*each.value.slack_channel" infrastructure-mgmt/main/targets.tf; then
    echo "  ✓ slack_channel is in secret version"
else
    echo "  ✗ FAIL: slack_channel missing from secret version"
    exit 1
fi

if grep -q "slack_bot_token.*=.*each.value.slack_bot_token" infrastructure-mgmt/main/targets.tf; then
    echo "  ✓ slack_bot_token is in secret version"
else
    echo "  ✗ FAIL: slack_bot_token missing from secret version"
    exit 1
fi

# Check that variables.tf has optional Slack fields
echo "✓ Checking variables.tf has optional Slack fields..."
if grep -q 'slack_channel.*=.*optional(string)' infrastructure-mgmt/main/variables.tf; then
    echo "  ✓ slack_channel is optional in variables"
else
    echo "  ✗ FAIL: slack_channel not marked as optional"
    exit 1
fi

if grep -q 'slack_bot_token.*=.*optional(string)' infrastructure-mgmt/main/variables.tf; then
    echo "  ✓ slack_bot_token is optional in variables"
else
    echo "  ✗ FAIL: slack_bot_token not marked as optional"
    exit 1
fi

# Check that webhook includes issues events
echo "✓ Checking webhook includes Issue events..."
if grep -q '"issues"' infrastructure-mgmt/main/targets.tf; then
    echo "  ✓ 'issues' event is in webhook configuration"
else
    echo "  ✗ FAIL: 'issues' event missing from webhook"
    exit 1
fi

if grep -q '"issue_comment"' infrastructure-mgmt/main/targets.tf; then
    echo "  ✓ 'issue_comment' event is in webhook configuration"
else
    echo "  ✗ FAIL: 'issue_comment' event missing from webhook"
    exit 1
fi

echo ""
echo "✅ All Terraform configuration tests passed!"
echo ""
echo "Summary of regression tests:"
echo "  - Slack fields are stored in AWS Secrets Manager"
echo "  - Slack fields are optional (backward compatible)"
echo "  - GitHub webhooks include Issue and Issue Comment events"
echo ""
echo "These tests would have caught:"
echo "  1. The 'not_authed' bug caused by missing secret fields"
echo "  2. Webhooks not triggering for Issues/Comments"
