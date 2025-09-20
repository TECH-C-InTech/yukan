package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const highlightEmbedColor = 0xb0b0b0

// Highlight represents a single summary item produced by the language model.
type Highlight struct {
	Title       string `json:"title"`
	Emoji       string `json:"emoji"`
	Description string `json:"description"`
	Link        string `json:"link"`
}

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
		filtered = append(filtered, h)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no valid highlights decoded")
	}
	return filtered, nil
}

func composeFinalMessage(highlights []Highlight, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if len(highlights) == 0 {
		if fallback == "" {
			return ""
		}
		return truncateRunes(fallback, messageCharBudget)
	}

	date := time.Now().Format("2006年1月2日")
	header := fmt.Sprintf("[**夕刊**] %s", date)
	return truncateRunes(header, messageCharBudget)
}

// BuildSummaryMessage prepares the Discord message header and embeds from highlights.
func BuildSummaryMessage(header string, highlights []Highlight, digests []channelDigest) (string, []*discordgo.MessageEmbed) {
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

		embed := &discordgo.MessageEmbed{
			Title:       title,
			Description: description,
			URL:         link,
			Color:       highlightEmbedColor,
		}
		if msg := findMessageByURL(digests, link); msg != nil {
			embed.Author = &discordgo.MessageEmbedAuthor{Name: msg.Author, IconURL: msg.AvatarURL}
		}
		embeds = append(embeds, embed)
	}

	if len(embeds) == 0 {
		return header, nil
	}

	return header, embeds
}

func findMessageByURL(digests []channelDigest, url string) *messageDigest {
	for i := range digests {
		for j := range digests[i].Messages {
			if digests[i].Messages[j].Link == url {
				return &digests[i].Messages[j]
			}
		}
	}
	return nil
}
