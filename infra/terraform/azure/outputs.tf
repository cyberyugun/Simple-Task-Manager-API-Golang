output "cluster_name" {
  value = azurerm_kubernetes_cluster.main.name
}

output "resource_group_name" {
  value = azurerm_resource_group.main.name
}

output "github_deploy_client_id" {
  value = azurerm_user_assigned_identity.github_deploy.client_id
}

output "tenant_id" {
  value = data.azurerm_client_config.current.tenant_id
}

output "subscription_id" {
  value = data.azurerm_client_config.current.subscription_id
}

output "postgres_fqdn" {
  value = azurerm_postgresql_flexible_server.main.fqdn
}

output "postgres_admin_password" {
  value     = random_password.postgres.result
  sensitive = true
}

output "redis_hostname" {
  value = azurerm_redis_cache.main.hostname
}

output "redis_primary_access_key" {
  value     = azurerm_redis_cache.main.primary_access_key
  sensitive = true
}
