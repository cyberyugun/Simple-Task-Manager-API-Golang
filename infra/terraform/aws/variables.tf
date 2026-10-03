variable "project_name" {
  description = "Short project identifier used in resource names."
  type        = string
  default     = "task-manager"
}

variable "environment" {
  description = "Deployment environment."
  type        = string
  default     = "production"
}

variable "aws_region" {
  description = "AWS region."
  type        = string
  default     = "ap-southeast-3"
}

variable "vpc_cidr" {
  description = "VPC CIDR."
  type        = string
  default     = "10.10.0.0/16"
}

variable "eks_kubernetes_version" {
  description = "EKS Kubernetes version. Keep this aligned with an AWS-supported version."
  type        = string
  default     = "1.34"
}

variable "eks_node_instance_types" {
  description = "Managed node group instance types."
  type        = list(string)
  default     = ["t3.medium"]
}

variable "eks_node_min_size" {
  type    = number
  default = 2
}

variable "eks_node_desired_size" {
  type    = number
  default = 2
}

variable "eks_node_max_size" {
  type    = number
  default = 5
}

variable "private_cluster" {
  description = "Disable the public EKS API endpoint. A private/self-hosted deployment runner is required when true."
  type        = bool
  default     = true
}

variable "eks_public_access_cidrs" {
  description = "Allowed CIDRs when private_cluster is false. Never use 0.0.0.0/0 in production."
  type        = list(string)
  default     = ["127.0.0.1/32"]
}

variable "github_repository" {
  description = "GitHub repository allowed to assume the production deploy role."
  type        = string
  default     = "cyberyugun/Simple-Task-Manager-API-Golang"
}

variable "github_oidc_provider_arn" {
  description = "ARN of the account-level GitHub Actions OIDC provider for token.actions.githubusercontent.com."
  type        = string
}

variable "postgres_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "postgres_engine_version" {
  type    = string
  default = "17"
}

variable "postgres_multi_az" {
  type    = bool
  default = true
}

variable "redis_node_type" {
  type    = string
  default = "cache.t4g.micro"
}

variable "redis_auth_token" {
  description = "Redis AUTH token. Store this outside Git and supply it through a secure Terraform variable source."
  type        = string
  sensitive   = true
}
