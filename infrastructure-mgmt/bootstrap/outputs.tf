output "tfstate_bucket_name" {
  description = "S3 bucket name for OpenTofu state"
  value       = aws_s3_bucket.tfstate.id
}

output "tfstate_lock_table_name" {
  description = "DynamoDB table name for state locking"
  value       = aws_dynamodb_table.tfstate_lock.name
}

output "aws_region" {
  description = "AWS region used"
  value       = var.aws_region
}
