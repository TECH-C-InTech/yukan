package summary

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

func truncateRunes(input string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(input) <= limit {
		return input
	}
	return string([]rune(input)[:limit]) + "…"
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

func buildMessageLink(guildID, channelID, messageID string) string {
	if guildID == "" || channelID == "" || messageID == "" {
		return ""
	}
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func isThreadChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread:
		return true
	default:
		return false
	}
}

func isThreadParentChannel(channelType discordgo.ChannelType) bool {
	switch channelType {
	case discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildForum:
		return true
	default:
		return false
	}
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

func userAvatarURL(user *discordgo.User) string {
	if user == nil {
		return ""
	}
	return user.AvatarURL("")
}
