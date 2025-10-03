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
		digests            []channelDigest
		fallbackBlocks     []string
		totalCollectedMsgs int
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
			continue
		default:
			totalCollectedMsgs += len(collected)
			digest := channelDigest{Name: display, Messages: collected}
			digests = append(digests, digest)
			fallbackBlocks = append(fallbackBlocks, renderChannelBlock(digest))
		}
	}

	fallback := strings.Join(fallbackBlocks, "\n\n")
	if strings.TrimSpace(fallback) == "" {
		if totalCollectedMsgs == 0 {
			fallback = "過去24時間のメッセージはありません"
		} else {
			fallback = "対象のテキストチャンネルが見つかりませんでした"
		}
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
