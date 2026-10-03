output "cluster_name" {
  value = aws_eks_cluster.main.name
}

output "aws_region" {
  value = var.aws_region
}

output "github_deploy_role_arn" {
  value = aws_iam_role.github_deploy.arn
}

output "postgres_endpoint" {
  value = aws_db_instance.postgres.address
}

output "postgres_master_secret_arn" {
  value     = try(aws_db_instance.postgres.master_user_secret[0].secret_arn, null)
  sensitive = true
}

output "redis_endpoint" {
  value = aws_elasticache_replication_group.redis.primary_endpoint_address
}
