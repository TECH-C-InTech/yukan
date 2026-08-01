package summary

import (
	"context"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Highlight represents a single summary item produced by the language model.
type Highlight struct {
	Title       string `json:"title"`
	Emoji       string `json:"emoji"`
	Description string `json:"description"`
	Link        string `json:"link"`
	Color       string `json:"color"`
}

// MessageDigest represents a single Discord message used for summarization.
type MessageDigest struct {
	Author         string
	AvatarURL      string
	Content        string
	Timestamp      time.Time
	MessageID      string
	ChannelID      string
	Link           string
	AttachmentURLs []string
}

// ChannelDigest aggregates messages per channel or thread.
type ChannelDigest struct {
	Name     string
	Messages []MessageDigest
}

// CollectorResult contains the data retrieved from Discord prior to summarization.
type CollectorResult struct {
	Digests       []ChannelDigest
	FallbackText  string
	TotalMessages int
	// SkippedChannels は権限不足などで読めずスキップしたチャンネル数
	SkippedChannels int
}

// Request describes a summary workflow trigger.
type Request struct {
	TargetName string
	GuildID    string
	ChannelID  string
	BotID      string
}

// Result contains the final message payload ready for posting.
type Result struct {
	Content       string
	Embeds        []*discordgo.MessageEmbed
	Highlights    []Highlight
	Digests       []ChannelDigest
	Fallback      string
	TotalMessages int
	WorkflowStart time.Time
	CompletedAt   time.Time
	Publishable   bool
}

// Config controls summary generation behavior.
type Config struct {
	MessageCharBudget int
	MaxHighlights     int
	MaxAttempts       int
}

// Summarizer は収集メッセージの要約を生成し、構造化ハイライトの生 JSON を返す。
// パース (ParseHighlights) はドメイン側で行う。
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) (string, error)
}
