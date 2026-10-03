variable "aws_region" {
  type    = string
  default = "ap-southeast-3"
}

variable "state_bucket_name" {
  description = "Globally unique S3 bucket name for Terraform state."
  type        = string
}

variable "state_key" {
  type    = string
  default = "production/terraform.tfstate"
}
