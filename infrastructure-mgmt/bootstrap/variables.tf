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
