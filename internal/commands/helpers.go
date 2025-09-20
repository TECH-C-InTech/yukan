package commands

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

const messageLineCharLimit = 120

func isThreadChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildPublicThread, discordgo.ChannelTypeGuildPrivateThread, discordgo.ChannelTypeGuildNewsThread:
		return true
	default:
		return false
	}
}

func isThreadParentChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews, discordgo.ChannelTypeGuildForum:
		return true
	default:
		return false
	}
}

func buildMessageLink(guildID, channelID, messageID string) string {
	if guildID == "" || channelID == "" || messageID == "" {
		return ""
	}
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func isTextChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildForum,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread:
		return true
	default:
		return false
	}
}

// サーバーニックネームを最優先し、段階的にフォールバックして名前を取得する。
func resolveAuthor(session *discordgo.Session, guildID string, msg *discordgo.Message, cache map[string]string) string {
	const unknown = "Unknown"
	if msg == nil {
		return unknown
	}

	// 小さなヘルパー：キャッシュに書いて返す
	// APIの呼び出しを抑制する
	putAndReturn := func(userID, name string) string {
		if cache != nil && userID != "" && name != "" {
			cache[userID] = name
		}
		return name
	}

	if name := displayNameFromMember(msg.Member); name != "" {
		uid := ""
		if msg.Author != nil {
			uid = msg.Author.ID
		}
		return putAndReturn(uid, name)
	}

	userID := ""
	if msg.Author != nil {
		userID = msg.Author.ID
	}

	if cache != nil && userID != "" {
		if cached, ok := cache[userID]; ok && cached != "" {
			return cached
		}
	}

	if session != nil && session.State != nil && guildID != "" && userID != "" {
		if member, err := session.State.Member(guildID, userID); err == nil {
			if name := displayNameFromMember(member); name != "" {
				return putAndReturn(userID, name)
			}
		}
	}

	if session != nil && guildID != "" && userID != "" {
		if member, err := session.GuildMember(guildID, userID); err == nil {
			if name := displayNameFromMember(member); name != "" {
				return putAndReturn(userID, name)
			}
		}
	}

	if msg.Author != nil {
		if name := displayNameFromUser(msg.Author); name != "" {
			return putAndReturn(userID, name)
		}
	}

	if cache != nil && userID != "" {
		cache[userID] = unknown
	}

	return unknown
}

func displayNameFromMember(member *discordgo.Member) string {
	if member == nil {
		return ""
	}
	if nick := strings.TrimSpace(member.Nick); nick != "" {
		return nick
	}
	return displayNameFromUser(member.User)
}

func displayNameFromUser(user *discordgo.User) string {
	if user == nil {
		return ""
	}
	if global := strings.TrimSpace(user.GlobalName); global != "" {
		return global
	}
	username := strings.TrimSpace(user.Username)
	if username == "" {
		return ""
	}
	discriminator := strings.TrimSpace(user.Discriminator)
	if discriminator != "" && discriminator != "0" {
		return fmt.Sprintf("%s#%s", username, discriminator)
	}
	return username
}

func extractContent(msg *discordgo.Message) string {
	if msg == nil {
		return "(内容なし)"
	}

	content := msg.Content
	if content == "" && len(msg.Attachments) > 0 {
		attachmentNames := make([]string, len(msg.Attachments))
		for i, att := range msg.Attachments {
			attachmentNames[i] = att.Filename
		}
		content = fmt.Sprintf("(添付ファイルあり: %s)", strings.Join(attachmentNames, ", "))
	}
	if content == "" {
		content = "(内容なし)"
	}

	return sanitizeContent(content)
}

func userAvatarURL(user *discordgo.User) string {
	if user == nil {
		return ""
	}
	return user.AvatarURL("")
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

func extractAttachmentURLs(msg *discordgo.Message) []string {
	if msg == nil || len(msg.Attachments) == 0 {
		return nil
	}

	urls := make([]string, 0, len(msg.Attachments))
	for _, attachment := range msg.Attachments {
		urls = append(urls, attachment.URL)
	}
	return urls
}
