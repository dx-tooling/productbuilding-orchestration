terraform {
  backend "s3" {
    bucket         = "productbuilder-tfstate-930067562391"
    key            = "main/terraform.tfstate"
    region         = "eu-central-1"
    dynamodb_table = "productbuilder-tfstate-lock"
    encrypt        = true
  }
}
