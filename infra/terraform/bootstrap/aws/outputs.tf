output "backend_config" {
  value = {
    bucket       = aws_s3_bucket.state.bucket
    key          = var.state_key
    region       = var.aws_region
    encrypt      = true
    use_lockfile = true
  }
}
