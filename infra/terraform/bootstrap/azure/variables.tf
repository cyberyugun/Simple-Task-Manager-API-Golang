variable "location" {
  type    = string
  default = "southeastasia"
}

variable "resource_group_name" {
  type    = string
  default = "rg-task-manager-terraform-state"
}

variable "storage_account_name" {
  description = "Globally unique lowercase storage account name, 3-24 alphanumeric characters."
  type        = string
}

variable "container_name" {
  type    = string
  default = "tfstate"
}

variable "state_key" {
  type    = string
  default = "production/terraform.tfstate"
}
