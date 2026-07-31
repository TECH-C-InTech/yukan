variable "project_id" {
  description = "GCP プロジェクト ID"
  type        = string
  default     = "yukan-discord-bot"
}

variable "region" {
  description = "リソースを配置するリージョン"
  type        = string
  default     = "asia-northeast1"
}
