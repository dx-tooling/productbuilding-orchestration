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

# ── Workspace-level LLM API key ──────────────────────────────────

resource "aws_secretsmanager_secret" "llm_api_key" {
  count = var.llm_api_key != "" ? 1 : 0
  name  = "${var.project_prefix}/llm-api-key"
}

resource "aws_secretsmanager_secret_version" "llm_api_key" {
  count         = var.llm_api_key != "" ? 1 : 0
  secret_id     = aws_secretsmanager_secret.llm_api_key[0].id
  secret_string = var.llm_api_key
}


# ── Per-target-repo resources: Secrets Manager secrets ──
#
# Per-target-repo GitHub-side resources (webhook, FIREWORKS_API_KEY Actions
# secret) are NOT created here; they are managed by the orchestrator process
# itself via its `targetadmin` reconciler at startup, using each target's PAT.
# This decouples target onboarding from the deployment's GitHub org binding,
# letting one orchestrator deployment manage targets across any number of
# GitHub orgs without provider-alias gymnastics.

# Store each target's credentials in Secrets Manager so cloud-init can stage
# them into /opt/orchestrator/targets.json and the orchestrator can load
# them on boot.
resource "aws_secretsmanager_secret" "target" {
  for_each = var.targets

  name = "${var.project_prefix}/targets/${each.value.repo_name}"
}

resource "aws_secretsmanager_secret_version" "target" {
  for_each = var.targets

  secret_id = aws_secretsmanager_secret.target[each.key].id
  secret_string = jsonencode({
    repo_owner        = each.value.repo_owner
    repo_name         = each.value.repo_name
    github_pat        = each.value.github_pat
    webhook_secret    = each.value.webhook_secret
    fireworks_api_key = each.value.fireworks_api_key
    slack_channel     = each.value.slack_channel
    slack_bot_token   = each.value.slack_bot_token
    language          = each.value.language
  })
}
