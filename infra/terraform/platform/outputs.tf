output "installed_components" {
  value = concat(
    [
      "cert-manager",
      "ingress-nginx",
      "metrics-server"
    ],
    var.enable_external_secrets ? ["external-secrets"] : [],
    var.enable_observability ? [
      "kube-prometheus-stack",
      "grafana",
      "tempo",
      "loki",
      "alloy",
      "otel-collector"
    ] : []
  )
}
