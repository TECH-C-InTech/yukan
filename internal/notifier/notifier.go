// Package notifier は運用者向けの Discord チャンネル通知を提供する。
package notifier

import (
	"context"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Notifier emits workflow events to operators.
type Notifier interface {
	Info(ctx context.Context, message string)
	Error(ctx context.Context, message string)
}

// DiscordNotifier posts informational messages to a Discord channel and stdout.
type DiscordNotifier struct {
	Session          *discordgo.Session
	ChannelID        string
	MaxMessageLength int
	TargetChannels   map[string]string
}

// Info logs informational events.
func (n *DiscordNotifier) Info(ctx context.Context, message string) {
	n.post(ctx, "info", message)
}

// Error logs failures.
func (n *DiscordNotifier) Error(ctx context.Context, message string) {
	n.post(ctx, "error", message)
}

func (n *DiscordNotifier) post(ctx context.Context, prefix, message string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}

	if n.MaxMessageLength > 0 && len([]rune(trimmed)) > n.MaxMessageLength {
		trimmed = string([]rune(trimmed)[:n.MaxMessageLength]) + "…"
	}

	channelID := n.ChannelID
	if target := TargetFromContext(ctx); target != "" {
		if mapped := n.TargetChannels[target]; mapped != "" {
			channelID = mapped
		}
	}

	if n.Session == nil || channelID == "" {
		return
	}
	if _, err := n.Session.ChannelMessageSend(channelID, trimmed); err != nil {
		slog.ErrorContext(ctx, "failed to post operator notification to Discord", "kind", prefix, "channel_id", channelID, "error", err)
	}
}
