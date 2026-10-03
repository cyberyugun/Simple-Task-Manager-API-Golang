output "backend_config" {
  value = {
    bucket = google_storage_bucket.state.name
    prefix = var.state_prefix
  }
}
