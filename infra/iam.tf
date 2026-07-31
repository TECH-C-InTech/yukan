# run.invoker はロール単位で authoritative に管理する (allUsers を確実に排除するため)
resource "google_cloud_run_v2_service_iam_binding" "invoker" {
  for_each = google_cloud_run_v2_service.yukan

  name     = each.value.name
  location = each.value.location
  role     = "roles/run.invoker"

  members = concat(
    [google_service_account.scheduler.member],
    # stg のみ手動検証用に naoki を許可
    each.key == "stg" ? ["user:nka21dev@gmail.com"] : []
  )
}

# CD (yukan-deployer@): イメージ push + Cloud Run デプロイのみ
resource "google_project_iam_member" "deployer_run_developer" {
  project = var.project_id
  role    = "roles/run.developer"
  member  = google_service_account.deployer.member
}

resource "google_artifact_registry_repository_iam_member" "deployer_ar_writer" {
  repository = google_artifact_registry_repository.yukan.name
  role       = "roles/artifactregistry.writer"
  member     = google_service_account.deployer.member
}

# デプロイ時にランタイム SA を紐付けるための actAs (yukan-run@ 限定)
resource "google_service_account_iam_member" "deployer_act_as_run" {
  service_account_id = google_service_account.run.name
  role               = "roles/iam.serviceAccountUser"
  member             = google_service_account.deployer.member
}

# ランタイム SA はシークレット2つの参照のみ (プロジェクトレベル権限なし)
resource "google_secret_manager_secret_iam_member" "run_secret_access" {
  for_each = {
    discord_bot_token = google_secret_manager_secret.discord_bot_token.secret_id
    gemini_api_key    = google_secret_manager_secret.gemini_api_key.secret_id
  }

  secret_id = each.value
  role      = "roles/secretmanager.secretAccessor"
  member    = google_service_account.run.member
}
