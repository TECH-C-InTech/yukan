resource "google_service_account" "run" {
  account_id   = "yukan-run"
  display_name = "yukan Cloud Run runtime"
  description  = "discord-yukan の実行用 (Secret Manager の参照のみ)"
}

resource "google_service_account" "scheduler" {
  account_id   = "yukan-scheduler"
  display_name = "yukan Cloud Scheduler invoker"
  description  = "Cloud Scheduler から Cloud Run を OIDC で起動する"
}

resource "google_service_account" "deployer" {
  account_id   = "yukan-deployer"
  display_name = "yukan CI deployer"
  description  = "GitHub Actions からのイメージ push と Cloud Run デプロイ"
}
