# Yukan

Yukan は Discord サーバー内の過去 24 時間分のメッセージを収集し、Gemini API を使って夕刊ダイジェストを生成・投稿する自動化ボットです。  
Cloud Scheduler などから `/tasks/daily-summary` を定期実行するだけで、決まったチャンネルにハイライトを届けられます。

## アーキテクチャ概要
```
Cloud Scheduler/Tasks ──► HTTP /tasks/daily-summary
                               │
                               ▼
                  summary.Service
                    ├─ collector (Discord API)
                    ├─ prompt builder
                    ├─ Gemini client
                    └─ presenter (embeds, fallback)
                               │
                               ├─ notifier (Discord log)
                               └─ Discord投稿
```

## セットアップ
1. Go 1.23+ と `gcloud` を用意します。
2. Discord Bot Token・Gemini API Key を取得し、Secret Manager などに保存します。
3. `.env` などで必要な環境変数を設定し、ローカルで起動できます:
   ```bash
   export DISCORD_BOT_TOKEN=xxxx
   export GEMINI_API_KEY=yyyy
   go run ./...
   ```
4. Bot を動かしたまま `curl -X POST http://localhost:8080/tasks/daily-summary?target=dev` を実行すると dev チャンネル向け夕刊を投稿します。

## 主要環境変数
| 変数 | 必須 | 説明 |
| --- | --- | --- |
| `DISCORD_BOT_TOKEN` | ✅ | Discord Bot のトークン |
| `GEMINI_API_KEY` | ✅ | Gemini API キー |
| `GEMINI_MODEL` |  | 使用するモデル (例: `gemini-2.5-flash`) |
| `PORT` |  | HTTP サーバーの待ち受けポート (既定: `8080`) |
| `YUKAN_LOG_CHANNEL_ID` |  | ログ通知先 Discord チャンネル ID |
| `YUKAN_DEFAULT_TARGET` |  | `/tasks/daily-summary` 未指定時に使うターゲット名 (既定: `prod`) |
| `YUKAN_SUMMARY_TARGETS` |  | `{"prod":{"guild_id":"...","channel_id":"..."},"dev":{...}}` の JSON。未指定時はソース既定値 |
| `YUKAN_SUMMARY_LOOKBACK_HOURS` |  | メッセージ収集のルックバック時間 (既定 24) |
| `YUKAN_SUMMARY_FETCH_LIMIT` |  | チャンネルごとのページングサイズ (既定 100) |
| `YUKAN_SUMMARY_MESSAGE_BUDGET` |  | 見出しメッセージ最大文字数 (既定 1800) |
| `YUKAN_SUMMARY_MAX_HIGHLIGHTS` |  | Gemini に要求する最大ハイライト数 (既定 3) |
| `YUKAN_SUMMARY_MAX_ATTEMPTS` |  | Gemini 呼び出しリトライ回数 (既定 5) |

## デプロイ
Google Cloud Run を想定しており、`Makefile` でビルド・デプロイできます。
```bash
make deploy
```
Secrets (`DISCORD_BOT_TOKEN`, `GEMINI_API_KEY`) は Secret Manager で管理し、`--set-secrets` オプションに合わせて指定してください。
