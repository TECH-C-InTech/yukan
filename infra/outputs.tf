output "wif_provider" {
  description = "GitHub Actions の google-github-actions/auth に渡す workload_identity_provider"
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "deployer_sa" {
  description = "アプリデプロイ用 SA"
  value       = google_service_account.deployer.email
}

output "tfaction_plan_sa" {
  description = "tfaction plan 用 SA"
  value       = google_service_account.tfaction_plan.email
}

output "tfaction_apply_sa" {
  description = "tfaction apply 用 SA"
  value       = google_service_account.tfaction_apply.email
}
