variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "eu-central-1"
}

variable "project_prefix" {
  description = "Prefix for resource naming"
  type        = string
  default     = "productbuilder"
}

variable "github_org" {
  description = "GitHub organization name"
  type        = string
}

variable "github_mgmt_pat" {
  description = "GitHub PAT for managing org resources (webhooks, secrets)"
  type        = string
  sensitive   = true
}

variable "preview_domain" {
  description = "Domain for preview environments"
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type for the orchestrator host"
  type        = string
  default     = "t3.xlarge"
}

variable "availability_zone" {
  description = "Availability zone for the orchestrator instance and its persistent state EBS volume. Pinned so the volume can re-attach across instance replacements. Defaults to <aws_region>a if null."
  type        = string
  default     = null
}

variable "state_volume_size_gb" {
  description = "Size of the persistent EBS volume that holds orchestrator SQLite + Traefik ACME cache. 20 GB is comfortable for a long-running deployment with a handful of targets."
  type        = number
  default     = 20
}

variable "ssh_public_key" {
  description = "SSH public key for EC2 instance access"
  type        = string
}

variable "slack_workspace" {
  description = "Slack workspace subdomain (e.g. 'myteam' for myteam.slack.com) for deep links"
  type        = string
  default     = ""
}

variable "slack_signing_secret" {
  description = "Slack app signing secret (from Basic Information page) for verifying Events API requests"
  type        = string
  sensitive   = true
  default     = ""
}

variable "llm_api_key" {
  description = "API key for the orchestration agent's primary LLM provider"
  type        = string
  sensitive   = true
  default     = ""
}

variable "acme_email" {
  description = "Email for Let's Encrypt ACME certificate registration"
  type        = string
  default     = "admin@example.com"
}

variable "orchestration_repo" {
  description = "Name of the orchestration repository (without org prefix)"
  type        = string
}

variable "orchestration_github_org" {
  description = "GitHub organization hosting the orchestration repo"
  type        = string
}

variable "targets" {
  description = "Map of target repositories to manage"
  type = map(object({
    repo_owner        = string
    repo_name         = string
    github_pat        = string
    webhook_secret    = string
    fireworks_api_key = string
    slack_channel     = optional(string)
    slack_bot_token   = optional(string)
    language          = optional(string)
  }))
  default = {}
}
