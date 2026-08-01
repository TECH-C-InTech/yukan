package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// Publisher は夕刊メッセージの Discord への投稿を担う。
type Publisher struct {
	Session *discordgo.Session
}

// Publish はヘッダーと embed 群を投稿する。
// embed はハイライト単位でリアクションを付けられるよう、1つずつ別メッセージで送る。
func (p *Publisher) Publish(_ context.Context, channelID, content string, embeds []*discordgo.MessageEmbed) error {
	if p == nil || p.Session == nil {
		return fmt.Errorf("publisher is not configured")
	}
	if content != "" {
		if _, err := p.Session.ChannelMessageSend(channelID, content); err != nil {
			return fmt.Errorf("failed to post header message: %w", err)
		}
	}
	for _, embed := range embeds {
		if embed == nil {
			continue
		}
		if _, err := p.Session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}}); err != nil {
			return fmt.Errorf("failed to post embed message: %w", err)
		}
	}
	return nil
}
