data "azurerm_client_config" "current" {}

output "backend_config" {
  value = {
    resource_group_name  = azurerm_resource_group.state.name
    storage_account_name = azurerm_storage_account.state.name
    container_name       = azurerm_storage_container.state.name
    key                  = var.state_key
    use_oidc             = true
    tenant_id            = data.azurerm_client_config.current.tenant_id
    subscription_id      = data.azurerm_client_config.current.subscription_id
  }
}
