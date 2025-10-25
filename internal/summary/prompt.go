package summary

import (
	"fmt"
	"strings"
)

func buildSummaryPrompt(digests []ChannelDigest, maxHighlights int) string {
	if len(digests) == 0 {
		return ""
	}
	if maxHighlights <= 0 {
		maxHighlights = 3
	}

	var builder strings.Builder
	builder.WriteString("あなたはDiscordサーバーの編集者です。以下は過去24時間に投稿されたメッセージ一覧です。\n")
	builder.WriteString(fmt.Sprintf("最も注目すべき出来事を最大%d件選び、日本語の夕刊ハイライトを作成してください。\n", maxHighlights))
	builder.WriteString("出力は必ず次のJSON配列のみとし、整形や説明文を入れないでください。\n")
	builder.WriteString("例: [{\"title\":\"見出し\",\"emoji\":\"\",\"description\":\"本文\",\"link\":\"https://discord.com/channels/...\",\"color\":\"#b0b0b0\"}]\n")
	builder.WriteString("各要素には title, emoji, description, link, color のキーを必ず含め、値が無い場合は空文字にしてください。\n")

	for _, digest := range digests {
		builder.WriteString("\n# ")
		builder.WriteString(digest.Name)
		builder.WriteString("\n")
		for i := len(digest.Messages) - 1; i >= 0; i-- {
			msg := digest.Messages[i]
			builder.WriteString("- ")
			builder.WriteString(msg.Timestamp.Format("01/02 15:04"))
			builder.WriteString(" ")
			builder.WriteString(msg.Author)
			builder.WriteString(" ")
			builder.WriteString(digest.Name)
			if msg.Link != "" {
				builder.WriteString(" ")
				builder.WriteString(msg.Link)
			}
			builder.WriteString(": ")
			builder.WriteString(msg.Content)
			builder.WriteString("\n")
		}
	}

	return builder.String()
}
