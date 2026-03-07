output "instance_public_ip" {
  description = "Elastic IP of the orchestrator instance"
  value       = aws_eip.orchestrator.public_ip
}

output "route53_zone_id" {
  description = "Route53 hosted zone ID for the preview domain"
  value       = aws_route53_zone.preview.zone_id
}

output "route53_nameservers" {
  description = "Nameservers to delegate from the apex domain hoster"
  value       = aws_route53_zone.preview.name_servers
}

output "preview_domain" {
  description = "Preview domain"
  value       = var.preview_domain
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.orchestrator.id
}
