package summary

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const defaultHighlightColor = 0xb0b0b0

// ParseHighlights decodes structured highlight output into sanitized values.
func ParseHighlights(raw string) ([]Highlight, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty highlight response")
	}
	var highlights []Highlight
	if err := json.Unmarshal([]byte(raw), &highlights); err != nil {
		return nil, fmt.Errorf("failed to decode highlight json: %w", err)
	}
	filtered := make([]Highlight, 0, len(highlights))
	for _, h := range highlights {
		h.Title = strings.TrimSpace(h.Title)
		h.Emoji = strings.TrimSpace(h.Emoji)
		h.Description = strings.TrimSpace(h.Description)
		h.Link = strings.TrimSpace(h.Link)
		if h.Title == "" || h.Description == "" || h.Link == "" {
			continue
		}
		h.Color = strings.TrimSpace(h.Color)
		filtered = append(filtered, h)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no valid highlights decoded")
	}
	return filtered, nil
}

func composeFinalMessage(highlights []Highlight, fallback string, budget int) string {
	fallback = strings.TrimSpace(fallback)
	if len(highlights) == 0 {
		if fallback == "" {
			return ""
		}
		return truncateRunes(fallback, budget)
	}
	date := time.Now().Format("2006年1月2日")
	header := fmt.Sprintf("[**夕刊**] %s", date)
	return truncateRunes(header, budget)
}

// BuildSummaryMessage prepares the Discord message header and embeds from highlights.
func BuildSummaryMessage(header string, highlights []Highlight, digests []ChannelDigest) (string, []*discordgo.MessageEmbed) {
	header = strings.TrimSpace(header)
	if len(highlights) == 0 {
		return header, nil
	}

	embeds := make([]*discordgo.MessageEmbed, 0, len(highlights))
	for _, highlight := range highlights {
		title := strings.TrimSpace(highlight.Title)
		description := strings.TrimSpace(highlight.Description)
		link := strings.TrimSpace(highlight.Link)
		emoji := strings.TrimSpace(highlight.Emoji)

		if title == "" || description == "" || link == "" {
			continue
		}

		if emoji != "" {
			title = fmt.Sprintf("%s %s", title, emoji)
		}

		color := resolveHighlightColor(highlight.Color)

		embed := &discordgo.MessageEmbed{
			Title:       title,
			Description: description,
			Color:       color,
		}
		// モデルが幻覚した Discord 外 URL をリンク化しない
		if isDiscordMessageLink(link) {
			embed.URL = link
			if msg := findMessageByURL(digests, link); msg != nil {
				embed.Author = &discordgo.MessageEmbedAuthor{Name: msg.Author, IconURL: msg.AvatarURL}
			}
		}
		embeds = append(embeds, embed)
	}

	if len(embeds) == 0 {
		return header, nil
	}

	return header, embeds
}

func isDiscordMessageLink(link string) bool {
	return strings.HasPrefix(link, "https://discord.com/channels/") ||
		strings.HasPrefix(link, "https://discordapp.com/channels/")
}

func findMessageByURL(digests []ChannelDigest, url string) *MessageDigest {
	for i := range digests {
		for j := range digests[i].Messages {
			if digests[i].Messages[j].Link == url {
				return &digests[i].Messages[j]
			}
		}
	}
	return nil
}

func resolveHighlightColor(raw string) int {
	if color, ok := parseHexColor(raw); ok {
		return color
	}
	return defaultHighlightColor
}

func parseHexColor(raw string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	if strings.HasPrefix(trimmed, "0x") || strings.HasPrefix(trimmed, "0X") {
		trimmed = trimmed[2:]
	}
	trimmed = strings.TrimPrefix(trimmed, "#")
	if len(trimmed) != 6 {
		return 0, false
	}
	value, err := strconv.ParseUint(trimmed, 16, 32)
	if err != nil {
		return 0, false
	}
	return int(value), true
}
