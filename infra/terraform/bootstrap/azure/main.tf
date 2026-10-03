resource "azurerm_resource_group" "state" {
  name     = var.resource_group_name
  location = var.location

  lifecycle {
    prevent_destroy = true
  }
}

resource "azurerm_storage_account" "state" {
  name                              = var.storage_account_name
  resource_group_name               = azurerm_resource_group.state.name
  location                          = azurerm_resource_group.state.location
  account_tier                      = "Standard"
  account_replication_type          = "GRS"
  min_tls_version                   = "TLS1_2"
  shared_access_key_enabled         = false
  public_network_access_enabled     = true
  infrastructure_encryption_enabled = true
  allow_nested_items_to_be_public   = false

  network_rules {
    default_action = "Deny"
    bypass         = ["AzureServices"]
    ip_rules       = var.allowed_ip_ranges
  }

  blob_properties {
    versioning_enabled = true

    delete_retention_policy {
      days = 30
    }

    container_delete_retention_policy {
      days = 30
    }
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "azurerm_storage_container" "state" {
  name                  = var.container_name
  storage_account_id    = azurerm_storage_account.state.id
  container_access_type = "private"
}
