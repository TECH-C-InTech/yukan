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
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].containers[0].image,
      client,
      client_version,
    ]
  }

  depends_on = [google_project_service.apis]
}
