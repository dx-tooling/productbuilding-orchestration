data "aws_caller_identity" "current" {}

locals {
  # Pin the AZ so the persistent EBS volume can reattach reliably across
  # instance replacements (e.g. AMI bumps). Defaults to <region>a.
  availability_zone = coalesce(var.availability_zone, "${var.aws_region}a")
}

# --- Route53 ---

resource "aws_route53_zone" "preview" {
  name = var.preview_domain
}

resource "aws_route53_record" "wildcard" {
  zone_id = aws_route53_zone.preview.zone_id
  name    = "*.${var.preview_domain}"
  type    = "A"
  ttl     = 300
  records = [aws_eip.orchestrator.public_ip]
}

# --- Networking ---

resource "aws_eip" "orchestrator" {
  domain = "vpc"

  tags = {
    Name = "${var.project_prefix}-orchestrator"
  }
}

resource "aws_eip_association" "orchestrator" {
  instance_id   = aws_instance.orchestrator.id
  allocation_id = aws_eip.orchestrator.id
}

resource "aws_security_group" "orchestrator" {
  name        = "${var.project_prefix}-orchestrator"
  description = "Security group for preview orchestrator"

  # HTTP
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # HTTPS
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # SSH
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # All outbound
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.project_prefix}-orchestrator"
  }
}

# --- IAM ---

resource "aws_iam_role" "orchestrator" {
  name = "${var.project_prefix}-orchestrator"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy" "orchestrator_secrets" {
  name = "secrets-access"
  role = aws_iam_role.orchestrator.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = [
          "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:${var.project_prefix}/targets/*",
          "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:${var.project_prefix}/slack-*",
          "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:${var.project_prefix}/llm-*"
        ]
      },
      {
        Effect   = "Allow"
        Action   = "secretsmanager:ListSecrets"
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role_policy" "orchestrator_route53" {
  name = "route53-dns01"
  role = aws_iam_role.orchestrator.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "route53:GetChange",
          "route53:ChangeResourceRecordSets",
          "route53:ListResourceRecordSets"
        ]
        Resource = [
          "arn:aws:route53:::hostedzone/${aws_route53_zone.preview.zone_id}",
          "arn:aws:route53:::change/*"
        ]
      },
      {
        Effect   = "Allow"
        Action   = "route53:ListHostedZonesByName"
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_instance_profile" "orchestrator" {
  name = "${var.project_prefix}-orchestrator"
  role = aws_iam_role.orchestrator.name
}

# --- SSH Key ---

resource "aws_key_pair" "orchestrator" {
  key_name   = "${var.project_prefix}-orchestrator"
  public_key = var.ssh_public_key
}

# --- EC2 Instance ---

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_instance" "orchestrator" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  availability_zone      = local.availability_zone
  key_name               = aws_key_pair.orchestrator.key_name
  iam_instance_profile   = aws_iam_instance_profile.orchestrator.name
  vpc_security_group_ids = [aws_security_group.orchestrator.id]

  user_data = templatefile("${path.module}/cloud-init.yml", {
    aws_region      = var.aws_region
    project_prefix  = var.project_prefix
    preview_domain  = var.preview_domain
    hosted_zone_id  = aws_route53_zone.preview.zone_id
    slack_workspace = var.slack_workspace
    acme_email      = var.acme_email
    repo_clone_url  = "https://x-access-token:${var.github_mgmt_pat}@github.com/${var.orchestration_github_org}/${var.orchestration_repo}.git"
  })

  root_block_device {
    volume_size = 80
    volume_type = "gp3"
  }

  tags = {
    Name = "${var.project_prefix}-orchestrator"
  }
}

# --- Persistent State Volume ---
# Survives instance replacement (e.g. AMI upgrades). Holds the orchestrator
# SQLite DB and Traefik's Let's Encrypt cert cache, mounted at
# /var/lib/orchestrator-state on the host. See cloud-init.yml for the
# first-boot format-and-mount sequence.

resource "aws_ebs_volume" "state" {
  availability_zone = local.availability_zone
  size              = var.state_volume_size_gb
  type              = "gp3"
  encrypted         = true

  tags = {
    Name = "${var.project_prefix}-orchestrator-state"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_volume_attachment" "state" {
  device_name                    = "/dev/sdf"
  volume_id                      = aws_ebs_volume.state.id
  instance_id                    = aws_instance.orchestrator.id
  stop_instance_before_detaching = true
}
