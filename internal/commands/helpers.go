package commands

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

const messageLineCharLimit = 120

func buildMessageLink(guildID, channelID, messageID string) string {
	if guildID == "" || channelID == "" || messageID == "" {
		return ""
	}
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func isTextChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews, discordgo.ChannelTypeGuildForum:
		return true
	default:
		return false
	}
}

func resolveAuthor(msg *discordgo.Message) string {
	if msg == nil || msg.Author == nil {
		return "Unknown"
	}
	if msg.Author.GlobalName != "" {
		return msg.Author.GlobalName
	}
	if msg.Author.Username != "" {
		return msg.Author.Username
	}
	return "Unknown"
}

func extractContent(msg *discordgo.Message) string {
	if msg == nil {
		return "(内容なし)"
	}

	content := msg.Content
	if content == "" && len(msg.Attachments) > 0 {
		content = fmt.Sprintf("添付ファイル (%s)", msg.Attachments[0].Filename)
	}
	if content == "" {
		content = "(内容なし)"
	}

	return sanitizeContent(content)
}

func sanitizeContent(input string) string {
	cleaned := strings.ReplaceAll(input, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "(内容なし)"
	}
	return cleaned
}

func truncateRunes(input string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(input) <= max {
		return input
	}

	runes := []rune(input)
	return string(runes[:max]) + "…"
}
