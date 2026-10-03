variable "project_id" {
  description = "GCP project ID."
  type        = string
}

variable "project_name" {
  type    = string
  default = "task-manager"
}

variable "environment" {
  type    = string
  default = "production"
}

variable "region" {
  type    = string
  default = "asia-southeast2"
}

variable "zone" {
  type    = string
  default = "asia-southeast2-a"
}

variable "network_cidr" {
  type    = string
  default = "10.30.0.0/20"
}

variable "gke_node_machine_type" {
  type    = string
  default = "e2-standard-2"
}

variable "gke_node_count" {
  type    = number
  default = 2
}

variable "private_cluster" {
  description = "Create a private GKE control-plane endpoint. Use a private/self-hosted deployment runner when true."
  type        = bool
  default     = true
}

variable "master_authorized_cidrs" {
  description = "CIDRs allowed to reach the GKE control plane when a public endpoint is enabled."
  type = list(object({
    cidr_block   = string
    display_name = string
  }))
  default = [
    {
      cidr_block   = "10.0.0.0/8"
      display_name = "private-networks"
    }
  ]
}

variable "github_repository" {
  type    = string
  default = "cyberyugun/Simple-Task-Manager-API-Golang"
}

variable "postgres_database_version" {
  type    = string
  default = "POSTGRES_16"
}

variable "postgres_tier" {
  type    = string
  default = "db-custom-1-3840"
}

variable "postgres_retained_backups" {
  description = "Number of successful Cloud SQL backups retained in addition to PITR logs."
  type        = number
  default     = 14

  validation {
    condition     = var.postgres_retained_backups >= 7 && var.postgres_retained_backups <= 365
    error_message = "postgres_retained_backups must be between 7 and 365."
  }
}

variable "redis_memory_size_gb" {
  type    = number
  default = 1
}
