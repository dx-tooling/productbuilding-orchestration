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
  default     = "luminor-project"
}

variable "github_mgmt_pat" {
  description = "GitHub PAT for managing org resources (webhooks, secrets)"
  type        = string
  sensitive   = true
}

variable "preview_domain" {
  description = "Domain for preview environments"
  type        = string
  default     = "productbuilder.luminor-tech.net"
}

variable "instance_type" {
  description = "EC2 instance type for the orchestrator host"
  type        = string
  default     = "t3.xlarge"
}

variable "ssh_public_key" {
  description = "SSH public key for EC2 instance access"
  type        = string
}

variable "targets" {
  description = "Map of target repositories to manage"
  type = map(object({
    repo_owner        = string
    repo_name         = string
    github_pat        = string
    webhook_secret    = string
    anthropic_api_key = string
  }))
  default = {}
}
