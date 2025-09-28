package commands

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

type messageDigest struct {
	Author         string
	AvatarURL      string
	Content        string
	Timestamp      time.Time
	MessageID      string
	ChannelID      string
	Link           string
	AttachmentURLs []string
}

type channelDigest struct {
	Name     string
	Messages []messageDigest
}

func collectLatestMessages(session *discordgo.Session, guildID string, botID string) ([]channelDigest, string, error) {
	if guildID == "" {
		return nil, "このコマンドはサーバー内のみで利用できます", nil
	}

	baseChannels, err := session.GuildChannels(guildID)
	if err != nil {
		return nil, "", fmt.Errorf("チャンネル一覧の取得に失敗しました: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(-lookbackWindow)

	// Build combined list including threads.
	var channels []*discordgo.Channel
	seen := make(map[string]struct{})
	parentNames := make(map[string]string)

	appendChannel := func(ch *discordgo.Channel) {
		if ch == nil {
			return
		}
		if _, exists := seen[ch.ID]; exists {
			return
		}
		seen[ch.ID] = struct{}{}
		channels = append(channels, ch)
	}

	for _, ch := range baseChannels {
		parentNames[ch.ID] = ch.Name
		appendChannel(ch)
	}

	if threads, err := session.GuildThreadsActive(guildID); err == nil && threads != nil {
		for _, th := range threads.Threads {
			appendChannel(th)
		}
	} else if err != nil {
		log.Printf("failed to fetch active threads: %v", err)
	}

	for _, ch := range baseChannels {
		if !isThreadParentChannel(ch.Type) {
			continue
		}
		threads, err := session.ThreadsArchived(ch.ID, nil, 100)
		if err != nil {
			log.Printf("failed to fetch archived threads for channel %s: %v", ch.ID, err)
			continue
		}
		if threads == nil {
			continue
		}
		for _, th := range threads.Threads {
			if th == nil || th.ThreadMetadata == nil {
				continue
			}
			if th.ThreadMetadata.ArchiveTimestamp.Before(cutoff) {
				continue
			}
			appendChannel(th)
		}
	}

	var (
		digests        []channelDigest
		fallbackBlocks []string
	)

	memberNames := make(map[string]string)

	for _, ch := range channels {
		if !isTextChannel(ch.Type) {
			continue
		}

		displayName := ch.Name
		if isThreadChannel(ch.Type) {
			if parent, ok := parentNames[ch.ParentID]; ok && parent != "" {
				displayName = fmt.Sprintf("%s › %s", parent, ch.Name)
			}
		}

		collected := make([]messageDigest, 0, fetchLimit)
		beforeID := ""
		failedFetch := false

		for {
			messages, err := session.ChannelMessages(ch.ID, fetchLimit, beforeID, "", "")
			if err != nil {
				log.Printf("failed to fetch messages for channel %s: %v", ch.ID, err)
				failedFetch = true
				break
			}
			if len(messages) == 0 {
				break
			}

			reachedOlderThanWindow := false
			for _, msg := range messages {
				timestamp := msg.Timestamp
				if timestamp.Before(cutoff) {
					reachedOlderThanWindow = true
					break
				}

				if botID != "" && msg.Author != nil && msg.Author.ID == botID {
					continue
				}

				digest := messageDigest{
					Author:         resolveAuthor(session, guildID, msg, memberNames),
					AvatarURL:      userAvatarURL(msg.Author),
					Content:        extractContent(msg),
					Timestamp:      timestamp,
					MessageID:      msg.ID,
					ChannelID:      ch.ID,
					Link:           buildMessageLink(guildID, ch.ID, msg.ID),
					AttachmentURLs: extractAttachmentURLs(msg),
				}

				collected = append(collected, digest)
			}

			if reachedOlderThanWindow || len(messages) < fetchLimit {
				break
			}

			beforeID = messages[len(messages)-1].ID
		}

		display := displayName
		switch {
		case failedFetch:
			fallbackBlocks = append(fallbackBlocks, fmt.Sprintf("#%s: メッセージを取得できませんでした", display))
		case len(collected) == 0:
			fallbackBlocks = append(fallbackBlocks, fmt.Sprintf("#%s: 過去24時間のメッセージはありません", display))
		default:
			digest := channelDigest{Name: display, Messages: collected}
			digests = append(digests, digest)
			fallbackBlocks = append(fallbackBlocks, renderChannelBlock(digest))
		}
	}

	fallback := strings.Join(fallbackBlocks, "\n\n")
	if strings.TrimSpace(fallback) == "" {
		fallback = "対象のテキストチャンネルが見つかりませんでした"
	}

	return digests, fallback, nil
}

func renderChannelBlock(digest channelDigest) string {
	lines := make([]string, 0, len(digest.Messages)+1)
	lines = append(lines, fmt.Sprintf("#%s", digest.Name))

	for i := len(digest.Messages) - 1; i >= 0; i-- {
		msg := digest.Messages[i]
		lines = append(lines, formatMessageLine(msg))
	}

	return strings.Join(lines, "\n")
}

func formatMessageLine(msg messageDigest) string {
	timestamp := msg.Timestamp.Format("01/02 15:04")
	link := msg.Link
	if link != "" {
		return fmt.Sprintf("- [%s] %s: %s (%s)", timestamp, msg.Author, truncateRunes(msg.Content, messageLineCharLimit), link)
	}
	return fmt.Sprintf("- [%s] %s: %s", timestamp, msg.Author, truncateRunes(msg.Content, messageLineCharLimit))
}

func buildSummaryPrompt(digests []channelDigest) string {
	if len(digests) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("あなたはDiscordサーバーの編集者です。以下は過去24時間に投稿されたメッセージ一覧です。最も注目すべき出来事を最大3件選び、日本語の夕刊ハイライトを作成してください。\n")
	builder.WriteString("\n出力要件:\n")
	builder.WriteString("- 出力はJSON配列のみとし、各要素は {\"title\": string, \"emoji\": string, \"description\": string, \"link\": string, \"color\": string} 形式のオブジェクトにすること。\n")
	builder.WriteString("- title・description・linkは必須。emojiは空文字でもよいが、合う絵文字があれば含めること。colorは #RRGGBB 形式で、そのハイライトの雰囲気に合う色を選ぶこと。\n")
	builder.WriteString("- titleには絵文字をつけないこと。\n")
	builder.WriteString("- descriptionは1～2文で要点と面白さを簡潔に伝えること。\n")
	builder.WriteString("- linkには必ず提供されたメッセージURLのうち関連する1件を使用すること。\n")
	builder.WriteString("- ハイライト数は重要度に応じて1～3件とすること。\n")
	builder.WriteString("- JSON以外のテキストは一切含めないこと。\n")

	builder.WriteString("\n選定ポリシー:\n")
	builder.WriteString("- 多様性制約: 原則として同一ユーザー/同一チャンネル/同一トピックの重複を避ける。\n")
	builder.WriteString("- トピック幅: 可能なら「成果・発表」「交流・雑談等」「小ネタ・ミーム/イベント告知」のように異なる系統から選ぶ。\n")
	builder.WriteString("- 反応指標: 返信数・リアクション・会話の継続・新規性を重視。ただし数が少なくても話題性が高いものは採用可。\n")
	builder.WriteString("- 事実は必ず提供メッセージ内からのみ。推測で人物名・数値・成果を追加しない。\n")

	builder.WriteString("\n除外/優先ルール:\n")
	builder.WriteString("- 除外: 運営・テスト・自動ログ・Bot設定や実装の技術的な詳細、個人への中傷/過度な炎上、NSFW。\n")
	builder.WriteString("- 内輪の短文のみ/リンクだけ/未成熟な下書きは原則不採用。\n")

	builder.WriteString("\n内部評価（出力に含めない）:\n")
	builder.WriteString("- 各候補を以下の4軸で0〜5点で評価し、総合上位から採用。ただし多様性制約を優先。\n")
	builder.WriteString("  1) 話題性/盛り上がり  2) 共有価値（みんなが知れて嬉しい）  3) 新規性/影響度  4) 深度/洞察度（議論の質や学びの多さ）\n")

	builder.WriteString("\nトーン/スタイル:\n")
	builder.WriteString("- 新聞の夕刊風に、軽やかで前向き。冷笑・皮肉・攻撃的な表現は避ける。\n")
	builder.WriteString("- タイトルは一目で内容が伝わるキャッチーさを優先（語呂・リズム・比喩は可、誤解を招く誇張は不可）。\n")

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
