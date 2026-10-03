resource "helm_release" "cert_manager" {
  name             = "cert-manager"
  namespace        = "cert-manager"
  create_namespace = true
  repository       = "https://charts.jetstack.io"
  chart            = "cert-manager"
  version          = var.cert_manager_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 600

  set = [
    {
      name  = "crds.enabled"
      value = "true"
    }
  ]
}

resource "helm_release" "ingress_nginx" {
  name             = "ingress-nginx"
  namespace        = "ingress-nginx"
  create_namespace = true
  repository       = "https://kubernetes.github.io/ingress-nginx"
  chart            = "ingress-nginx"
  version          = var.ingress_nginx_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 600

  set = [
    {
      name  = "controller.ingressClassResource.name"
      value = "nginx"
    },
    {
      name  = "controller.ingressClass"
      value = "nginx"
    },
    {
      name  = "controller.metrics.enabled"
      value = "true"
    }
  ]
}

resource "helm_release" "metrics_server" {
  name             = "metrics-server"
  namespace        = "kube-system"
  create_namespace = false
  repository       = "https://kubernetes-sigs.github.io/metrics-server"
  chart            = "metrics-server"
  version          = var.metrics_server_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 600
}

resource "helm_release" "external_secrets" {
  count = var.enable_external_secrets ? 1 : 0

  name             = "external-secrets"
  namespace        = "external-secrets"
  create_namespace = true
  repository       = "https://charts.external-secrets.io"
  chart            = "external-secrets"
  version          = var.external_secrets_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 600

  set = [
    {
      name  = "installCRDs"
      value = "true"
    }
  ]
}

resource "helm_release" "kube_prometheus_stack" {
  count = var.enable_observability ? 1 : 0

  name             = "kube-prometheus-stack"
  namespace        = "monitoring"
  create_namespace = true
  repository       = "https://prometheus-community.github.io/helm-charts"
  chart            = "kube-prometheus-stack"
  version          = var.kube_prometheus_stack_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 900

  values = [
    yamlencode({
      grafana = {
        enabled = true
        sidecar = {
          dashboards = {
            enabled         = true
            searchNamespace = "ALL"
          }
        }
      }
      prometheus = {
        prometheusSpec = {
          serviceMonitorSelectorNilUsesHelmValues = false
          serviceMonitorNamespaceSelector         = {}
          ruleSelectorNilUsesHelmValues           = false
          ruleNamespaceSelector                   = {}
          retention                               = "15d"
        }
      }
    })
  ]
}

resource "helm_release" "tempo" {
  count = var.enable_observability ? 1 : 0

  name             = "tempo"
  namespace        = "monitoring"
  create_namespace = true
  repository       = "https://grafana.github.io/helm-charts"
  chart            = "tempo"
  version          = var.tempo_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 900
  values          = [var.tempo_values_yaml]
}

resource "helm_release" "loki" {
  count = var.enable_observability ? 1 : 0

  name             = "loki"
  namespace        = "monitoring"
  create_namespace = true
  repository       = "https://grafana.github.io/helm-charts"
  chart            = "loki"
  version          = var.loki_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 900
  values          = [var.loki_values_yaml]
}

resource "helm_release" "alloy" {
  count = var.enable_observability ? 1 : 0

  name             = "alloy"
  namespace        = "monitoring"
  create_namespace = true
  repository       = "https://grafana.github.io/helm-charts"
  chart            = "alloy"
  version          = var.alloy_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 900
  values          = [var.alloy_values_yaml]

  depends_on = [helm_release.loki]
}

resource "helm_release" "otel_collector" {
  count = var.enable_observability ? 1 : 0

  name             = "otel-collector"
  namespace        = "monitoring"
  create_namespace = true
  repository       = "https://open-telemetry.github.io/opentelemetry-helm-charts"
  chart            = "opentelemetry-collector"
  version          = var.opentelemetry_collector_chart_version

  atomic          = true
  cleanup_on_fail = true
  wait            = true
  timeout         = 900

  values = [
    yamlencode({
      fullnameOverride = "otel-collector"
      mode             = "deployment"
      config = {
        receivers = {
          otlp = {
            protocols = {
              grpc = {
                endpoint = "0.0.0.0:4317"
              }
              http = {
                endpoint = "0.0.0.0:4318"
              }
            }
          }
        }
        processors = {
          batch = {}
          memory_limiter = {
            check_interval = "5s"
            limit_mib      = 256
          }
        }
        exporters = {
          "otlphttp/tempo" = {
            endpoint = "http://tempo:4318"
          }
        }
        service = {
          pipelines = {
            traces = {
              receivers  = ["otlp"]
              processors = ["memory_limiter", "batch"]
              exporters  = ["otlphttp/tempo"]
            }
          }
        }
      }
    })
  ]

  depends_on = [helm_release.tempo]
}
