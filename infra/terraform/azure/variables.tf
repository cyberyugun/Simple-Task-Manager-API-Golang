variable "project_name" {
  type    = string
  default = "task-manager"
}

variable "environment" {
  type    = string
  default = "production"
}

variable "location" {
  description = "Azure region."
  type        = string
  default     = "southeastasia"
}

variable "vnet_cidr" {
  type    = string
  default = "10.20.0.0/16"
}

variable "aks_kubernetes_version" {
  description = "Optional AKS Kubernetes version. Null lets Azure select a supported default."
  type        = string
  default     = null
}

variable "aks_node_vm_size" {
  type    = string
  default = "Standard_D2s_v5"
}

variable "aks_node_count" {
  type    = number
  default = 3
}

variable "aks_node_zones" {
  description = "Availability zones used by the AKS system node pool."
  type        = list(string)
  default     = ["1", "2", "3"]

  validation {
    condition     = length(var.aks_node_zones) >= 2
    error_message = "aks_node_zones must contain at least two zones for production HA."
  }
}

variable "private_cluster" {
  description = "Create a private AKS API endpoint. Use a private/self-hosted GitHub deployment runner when true."
  type        = bool
  default     = true
}

variable "github_repository" {
  type    = string
  default = "cyberyugun/Simple-Task-Manager-API-Golang"
}

variable "postgres_sku_name" {
  type    = string
  default = "GP_Standard_D2s_v3"
}

variable "postgres_version" {
  type    = string
  default = "16"
}

variable "postgres_backup_retention_days" {
  description = "PostgreSQL Flexible Server backup retention for PITR."
  type        = number
  default     = 35

  validation {
    condition     = var.postgres_backup_retention_days >= 7 && var.postgres_backup_retention_days <= 35
    error_message = "postgres_backup_retention_days must be between 7 and 35."
  }
}

variable "redis_capacity" {
  description = "Premium Redis capacity."
  type        = number
  default     = 1
}
