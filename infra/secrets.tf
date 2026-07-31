# シークレットの「箱」のみ Terraform 管理。バージョン (値) は gcloud で手動投入する。
resource "google_secret_manager_secret" "discord_bot_token" {
  secret_id = "discord-bot-token"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "gemini_api_key" {
  secret_id = "gemini-api-key"

  replication {
    auto {}
  }
}
