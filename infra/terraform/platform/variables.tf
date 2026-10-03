variable "kubeconfig_path" {
  description = "Path to kubeconfig for the target production cluster."
  type        = string
}

variable "kube_context" {
  description = "Optional kubeconfig context."
  type        = string
  default     = null
}

variable "cert_manager_chart_version" {
  description = "Exact cert-manager Helm chart version."
  type        = string
}

variable "ingress_nginx_chart_version" {
  description = "Exact ingress-nginx Helm chart version."
  type        = string
}

variable "metrics_server_chart_version" {
  description = "Exact metrics-server Helm chart version."
  type        = string
}

variable "enable_external_secrets" {
  description = "Install External Secrets Operator."
  type        = bool
  default     = true
}

variable "external_secrets_chart_version" {
  description = "Exact external-secrets Helm chart version when enabled."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_external_secrets || var.external_secrets_chart_version != null
    error_message = "external_secrets_chart_version is required when enable_external_secrets is true."
  }
}
