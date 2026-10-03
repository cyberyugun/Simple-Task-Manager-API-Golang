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

variable "enable_observability" {
  description = "Install the production observability stack."
  type        = bool
  default     = false
}

variable "kube_prometheus_stack_chart_version" {
  description = "Exact kube-prometheus-stack Helm chart version."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_observability || var.kube_prometheus_stack_chart_version != null
    error_message = "kube_prometheus_stack_chart_version is required when enable_observability is true."
  }
}

variable "tempo_chart_version" {
  description = "Exact Grafana Tempo Helm chart version."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_observability || var.tempo_chart_version != null
    error_message = "tempo_chart_version is required when enable_observability is true."
  }
}

variable "tempo_values_yaml" {
  description = "Production Tempo values. Configure durable object storage and retention."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = !var.enable_observability || (var.tempo_values_yaml != null && length(trimspace(var.tempo_values_yaml)) > 0)
    error_message = "tempo_values_yaml is required when enable_observability is true."
  }
}

variable "loki_chart_version" {
  description = "Exact Grafana Loki Helm chart version."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_observability || var.loki_chart_version != null
    error_message = "loki_chart_version is required when enable_observability is true."
  }
}

variable "loki_values_yaml" {
  description = "Production Loki values. Configure durable object storage, retention, and limits."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = !var.enable_observability || (var.loki_values_yaml != null && length(trimspace(var.loki_values_yaml)) > 0)
    error_message = "loki_values_yaml is required when enable_observability is true."
  }
}

variable "alloy_chart_version" {
  description = "Exact Grafana Alloy Helm chart version."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_observability || var.alloy_chart_version != null
    error_message = "alloy_chart_version is required when enable_observability is true."
  }
}

variable "alloy_values_yaml" {
  description = "Grafana Alloy values used to collect Kubernetes JSON logs and send them to Loki."
  type        = string
  default     = null
  sensitive   = true

  validation {
    condition     = !var.enable_observability || (var.alloy_values_yaml != null && length(trimspace(var.alloy_values_yaml)) > 0)
    error_message = "alloy_values_yaml is required when enable_observability is true."
  }
}

variable "opentelemetry_collector_chart_version" {
  description = "Exact OpenTelemetry Collector Helm chart version."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_observability || var.opentelemetry_collector_chart_version != null
    error_message = "opentelemetry_collector_chart_version is required when enable_observability is true."
  }
}
