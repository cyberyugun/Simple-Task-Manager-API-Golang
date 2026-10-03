output "installed_components" {
  value = concat(
    [
      "cert-manager",
      "ingress-nginx",
      "metrics-server"
    ],
    var.enable_external_secrets ? ["external-secrets"] : []
  )
}
