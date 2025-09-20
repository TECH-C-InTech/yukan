package commands

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

// Summarizer generates structured highlights from the collected messages.
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) ([]Highlight, error)
}

// GenerateSummaryForGuild collects the latest messages and produces a final digest message.
func GenerateSummaryForGuild(ctx context.Context, session *discordgo.Session, summarizer Summarizer, guildID string) (string, []Highlight, []channelDigest, error) {
	if session == nil {
		return "", nil, nil, fmt.Errorf("session is nil")
	}

	botID := ""
	if session.State != nil && session.State.User != nil {
		botID = session.State.User.ID
	}

	digests, fallback, err := collectLatestMessages(session, guildID, botID)
	if err != nil {
		return "", nil, nil, err
	}

	finalMessage := fallback
	var highlights []Highlight
	if len(digests) > 0 && summarizer != nil {
		summaryCtx := ctx
		if summaryCtx == nil {
			summaryCtx = context.Background()
		}
		if _, hasDeadline := summaryCtx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			summaryCtx, cancel = context.WithTimeout(summaryCtx, 45*time.Second)
			defer cancel()
		}

		prompt := buildSummaryPrompt(digests)
		if prompt != "" {
			summaryHighlights, err := summarizer.Summarize(summaryCtx, prompt)
			if err != nil {
				log.Printf("failed to summarize messages: %v", err)
			} else if len(summaryHighlights) > 0 {
				highlights = summaryHighlights
				finalMessage = composeFinalMessage(highlights, fallback)
			}
		}
	}

	if strings.TrimSpace(finalMessage) == "" {
		finalMessage = fallback
	}

	if utf8.RuneCountInString(finalMessage) > messageCharBudget {
		finalMessage = truncateRunes(finalMessage, messageCharBudget)
	}

	return finalMessage, highlights, digests, nil
}
