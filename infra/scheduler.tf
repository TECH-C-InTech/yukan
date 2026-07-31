resource "google_cloud_scheduler_job" "daily" {
  name      = "yukan-daily-summary"
  region    = var.region
  schedule  = "0 19 * * *"
  time_zone = "Asia/Tokyo"

  # 要約は数分かかる。デフォルト 180s だと完了前に打ち切られ、無言リトライ → 重複投稿の原因になる
  attempt_deadline = "900s"

  retry_config {
    retry_count          = 3
    min_backoff_duration = "10s"
    max_backoff_duration = "300s"
    max_doublings        = 5
    max_retry_duration   = "900s"
  }

  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.yukan["prod"].uri}/tasks/daily-summary?target=prod"

    oidc_token {
      service_account_email = google_service_account.scheduler.email
      # audience はパス・クエリを含まないサービスのベース URL であること
      audience = google_cloud_run_v2_service.yukan["prod"].uri
    }
  }

  depends_on = [google_project_service.apis]
}
