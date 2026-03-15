# ── Workspace-level Slack secret ─────────────────────────────────

resource "aws_secretsmanager_secret" "slack_signing_secret" {
  count = var.slack_signing_secret != "" ? 1 : 0
  name  = "${var.project_prefix}/slack-signing-secret"
}

resource "aws_secretsmanager_secret_version" "slack_signing_secret" {
  count         = var.slack_signing_secret != "" ? 1 : 0
  secret_id     = aws_secretsmanager_secret.slack_signing_secret[0].id
  secret_string = var.slack_signing_secret
}

# ── Workspace-level Fireworks API key ────────────────────────────

resource "aws_secretsmanager_secret" "fireworks_api_key" {
  count = var.fireworks_api_key != "" ? 1 : 0
  name  = "${var.project_prefix}/fireworks-api-key"
}

resource "aws_secretsmanager_secret_version" "fireworks_api_key" {
  count         = var.fireworks_api_key != "" ? 1 : 0
  secret_id     = aws_secretsmanager_secret.fireworks_api_key[0].id
  secret_string = var.fireworks_api_key
}

# ── Per-target-repo resources: Secrets Manager secrets, GitHub webhooks, GitHub Actions secrets ──

# Store each target's credentials in Secrets Manager
resource "aws_secretsmanager_secret" "target" {
  for_each = var.targets

  name = "${var.project_prefix}/targets/${each.value.repo_name}"
}

resource "aws_secretsmanager_secret_version" "target" {
  for_each = var.targets

  secret_id = aws_secretsmanager_secret.target[each.key].id
  secret_string = jsonencode({
    repo_owner      = each.value.repo_owner
    repo_name       = each.value.repo_name
    github_pat      = each.value.github_pat
    webhook_secret  = each.value.webhook_secret
    slack_channel   = each.value.slack_channel
    slack_bot_token = each.value.slack_bot_token
  })
}

# Create webhook on each target repo for PR and Issue events
# This enables both preview environments and Slack notifications
resource "github_repository_webhook" "preview" {
  for_each = var.targets

  repository = each.value.repo_name

  configuration {
    url          = "https://api.${var.preview_domain}/webhook"
    content_type = "json"
    secret       = each.value.webhook_secret
    insecure_ssl = false
  }

  active = true
  events = ["pull_request", "issues", "issue_comment"]
}

# Set FIREWORKS_API_KEY as a GitHub Actions secret on each target repo
resource "github_actions_secret" "fireworks_api_key" {
  for_each = var.targets

  repository      = each.value.repo_name
  secret_name     = "FIREWORKS_API_KEY"
  plaintext_value = each.value.fireworks_api_key
}
