output "cluster_name" {
  value = google_container_cluster.main.name
}

output "gke_location" {
  value = google_container_cluster.main.location
}

output "github_workload_identity_provider" {
  value = google_iam_workload_identity_pool_provider.github.name
}

output "github_deploy_service_account" {
  value = google_service_account.github_deploy.email
}

output "postgres_private_ip" {
  value = google_sql_database_instance.postgres.private_ip_address
}

output "postgres_password" {
  value     = random_password.postgres.result
  sensitive = true
}

output "redis_host" {
  value = google_redis_instance.main.host
}

output "redis_auth_string" {
  value     = google_redis_instance.main.auth_string
  sensitive = true
}
