variable "enable_staging" {
  description = "リファクタリング期間中の一時 staging 環境を作成するか"
  type        = bool
  default     = true
}

variable "image" {
  description = "デプロイするコンテナイメージ (通常は CD が SHA タグ付きで更新するため ignore_changes 対象)"
  type        = string
  default     = "asia-northeast1-docker.pkg.dev/yukan-discord-bot/yukan-bot/discord-yukan"
}

# 夕刊の投稿先。チャンネル ID は秘匿情報ではないためここで管理する
variable "summary_targets" {
  description = "YUKAN_SUMMARY_TARGETS に渡す投稿先定義"
  type = map(object({
    guild_id        = string
    source_guild_id = string
    channel_id      = string
    log_channel_id  = string
  }))
  default = {
    prod = {
      guild_id        = "1239827951667908668"
      source_guild_id = "1239827951667908668"
      channel_id      = "1418904371579719710"
      log_channel_id  = "1430142792314650764"
    }
    dev = {
      guild_id        = "1239827951667908668"
      source_guild_id = "1239827951667908668"
      channel_id      = "1417771223433351170"
      log_channel_id  = "1417771223433351170"
    }
  }
}

locals {
  services = merge(
    {
      prod = {
        name           = "discord-yukan"
        default_target = "prod"
      }
    },
    var.enable_staging ? {
      stg = {
        name           = "discord-yukan-stg"
        default_target = "dev"
      }
    } : {}
  )
}

resource "google_cloud_run_v2_service" "yukan" {
  for_each = local.services

  name     = each.value.name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  # stg は一時環境で enable_staging=false により破棄されるため保護しない
  deletion_protection = each.key == "prod"

  template {
    service_account                  = google_service_account.run.email
    timeout                          = "900s"
    max_instance_request_concurrency = 80

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      image = var.image

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1000m"
          memory = "512Mi"
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      env {
        name = "DISCORD_BOT_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.discord_bot_token.secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GEMINI_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.gemini_api_key.secret_id
            version = "latest"
          }
        }
      }

      env {
        name  = "YUKAN_DEFAULT_TARGET"
        value = each.value.default_target
      }

      env {
        name  = "YUKAN_SUMMARY_TARGETS"
        value = jsonencode(var.summary_targets)
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].containers[0].image,
      client,
      client_version,
    ]
  }

  # ランタイム SA がシークレットを読めるようになってからリビジョンを作る
  depends_on = [
    google_project_service.apis,
    google_secret_manager_secret_iam_member.run_secret_access,
  ]
}

# apply トリガー用コメント: stg 再作成 (2026-07-31)
