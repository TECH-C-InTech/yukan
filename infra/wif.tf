variable "github_repository" {
  description = "WIF で信頼する GitHub リポジトリ (owner/repo)"
  type        = string
  default     = "TECH-C-InTech/yukan"
}

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"
}

resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-oidc"
  display_name                       = "GitHub OIDC"

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
  }

  # このリポジトリ以外からのトークン発行を拒否する (必須のガード)
  attribute_condition = "assertion.repository == \"${var.github_repository}\""

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

locals {
  github_principal = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_repository}"
}

# tfaction 用 SA: plan は読み取り、apply は書き込みで分離
resource "google_service_account" "tfaction_plan" {
  account_id   = "yukan-tfaction-plan"
  display_name = "tfaction terraform plan"
  description  = "PR 上での terraform plan (読み取り専用)"
}

resource "google_service_account" "tfaction_apply" {
  account_id   = "yukan-tfaction-apply"
  display_name = "tfaction terraform apply"
  description  = "main マージ後の terraform apply (書き込み)"
}

# GitHub Actions からの impersonation を許可
resource "google_service_account_iam_member" "wif_impersonation" {
  for_each = {
    deployer      = google_service_account.deployer.name
    tfaction_plan = google_service_account.tfaction_plan.name
    tfaction_appl = google_service_account.tfaction_apply.name
  }

  service_account_id = each.value
  role               = "roles/iam.workloadIdentityUser"
  member             = local.github_principal
}

# plan SA: プロジェクト読み取り + state バケットの読み書き (lock 用)
resource "google_project_iam_member" "tfaction_plan_viewer" {
  project = var.project_id
  role    = "roles/viewer"
  member  = google_service_account.tfaction_plan.member
}

# apply SA: リソース書き込み + IAM バインディング管理
resource "google_project_iam_member" "tfaction_apply_roles" {
  for_each = toset([
    "roles/editor",
    "roles/resourcemanager.projectIamAdmin",
    "roles/iam.workloadIdentityPoolAdmin",
    # editor に含まれない setIamPolicy 系
    "roles/secretmanager.admin",
    "roles/run.admin",
    "roles/artifactregistry.admin",
    "roles/iam.serviceAccountAdmin",
  ])

  project = var.project_id
  role    = each.value
  member  = google_service_account.tfaction_apply.member
}

# state バケットへのアクセス (バケット自体は Terraform 管理外)
resource "google_storage_bucket_iam_member" "tfstate_access" {
  for_each = {
    plan  = google_service_account.tfaction_plan.member
    apply = google_service_account.tfaction_apply.member
  }

  bucket = "yukan-discord-bot-tfstate"
  role   = "roles/storage.admin"
  member = each.value
}
