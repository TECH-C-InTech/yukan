# infra

yukan の GCP インフラ (Cloud Run / Cloud Scheduler / Secret Manager / Artifact Registry / IAM / WIF) を Terraform で管理する。

## 運用フロー

- **PR を出す** → `terraform-plan` ワークフローが plan を実行し、tfcmt が結果を PR コメントに貼る
- **main にマージ** → `terraform-apply` ワークフローが自動 apply する
- ローカルからの `terraform apply` はブートストラップ時以外は原則行わない

## 構成の要点

| 項目 | 内容 |
| --- | --- |
| state | `gs://yukan-discord-bot-tfstate` (バージョニング有効、**手動作成・Terraform 管理外**) |
| 認証 | GitHub Actions → WIF (`github` プール)。`TECH-C-InTech/yukan` 以外のリポジトリは attribute_condition で拒否 |
| SA 分離 | plan: `yukan-tfaction-plan@` (viewer) / apply: `yukan-tfaction-apply@` (editor + projectIamAdmin) / CD: `yukan-deployer@` / ランタイム: `yukan-run@` (シークレット参照のみ) / Scheduler: `yukan-scheduler@` |
| Cloud Run | `discord-yukan` (prod) + `discord-yukan-stg` (staging、`enable_staging` で出し分け)。**`allUsers` invoker は authoritative binding で排除済み — 復活させないこと** |
| Scheduler | prod のみ 19:00 JST。`yukan-scheduler@` の OIDC トークンで起動 (audience はサービスのベース URL、パス・クエリを含めない) |

## staging の手動トリガー

staging にはスケジューラが無い。検証するときは自分の ID トークンで叩く (naoki のアカウントに stg 限定の invoker が付与済み):

```bash
STG_URL=$(gcloud run services describe discord-yukan-stg --region asia-northeast1 --project yukan-discord-bot --format='value(status.url)')
curl -X POST -H "Authorization: Bearer $(gcloud auth print-identity-token)" "$STG_URL/tasks/daily-summary?target=dev"
```

夕刊は dev チャンネルに投稿される。

## ロールバック

アプリの不具合時は直前リビジョンに戻すのが最速:

```bash
gcloud run revisions list --service discord-yukan --region asia-northeast1 --project yukan-discord-bot
gcloud run services update-traffic discord-yukan --region asia-northeast1 --project yukan-discord-bot --to-revisions <REVISION>=100
```

インフラは該当 PR を revert してマージすれば tfaction が逆向きに apply する。

## ブートストラップ (再構築時のみ)

1. state バケット作成: `gcloud storage buckets create gs://yukan-discord-bot-tfstate --location asia-northeast1 --uniform-bucket-level-access` + バージョニング有効化
2. `terraform init`
3. 既存リソースがあれば import (再作成させない):
   ```bash
   terraform import google_artifact_registry_repository.yukan projects/yukan-discord-bot/locations/asia-northeast1/repositories/yukan-bot
   terraform import google_secret_manager_secret.discord_bot_token projects/yukan-discord-bot/secrets/discord-bot-token
   terraform import google_secret_manager_secret.gemini_api_key projects/yukan-discord-bot/secrets/gemini-api-key
   terraform import 'google_cloud_run_v2_service.yukan["prod"]' projects/yukan-discord-bot/locations/asia-northeast1/services/discord-yukan
   ```
4. `terraform plan` が意図した差分だけになるまで調整 → apply
5. シークレットの値は Terraform 管理外。投入は `gcloud secrets versions add`

## ハマりどころメモ (tfaction)

- 各 working directory に `tfaction.yaml` (マーカー) が必要
- `target_groups[].working_directory` は末尾スラッシュなしで書く
- tfaction v2 は GCP 認証を自動実行しない → setup の outputs を使って `google-github-actions/auth` を挟む
- action 名は `plan` / `apply`
- `available_providers` に使用プロバイダの登録が必要
- `.terraform.lock.hcl` は `terraform providers lock -platform=...` で全プラットフォーム分をコミットしておく (CI からの push 権限が不要になる)
- **plan ワークフローの完了前にマージしない** — apply は PR の plan 結果 (Artifact) をそのまま適用するため、plan 未完了でマージすると `Artifact not found` で落ちる。ブランチ保護で plan を必須チェックにするのが確実
- 作成に失敗して tainted になった Cloud Run サービスは `deletion_protection` の鶏卵問題で replace できない → `terraform untaint` して in-place 更新に切り替える
- apply SA へのロール付与とそのロールを使う操作を同じ apply に入れると IAM 伝播ラグで失敗する → 次の PR で再適用すれば通る
