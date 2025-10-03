package commands

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
)

const geminiMismatchChannelID = "1295592548227616768"

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
			const maxAttempts = 5
			for attempts := 0; attempts < maxAttempts && summaryCtx.Err() == nil; attempts++ {
				summaryHighlights, err := summarizer.Summarize(summaryCtx, prompt)
				if err != nil {
					var rawErr interface{ RawResponse() string }
					if errors.As(err, &rawErr) && rawErr != nil {
						postGeminiMismatch(session, rawErr.RawResponse())
						continue
					}
					log.Printf("failed to summarize messages: %v", err)
					break
				}
				if len(summaryHighlights) == 0 {
					log.Printf("failed to summarize messages: gemini returned no highlights")
					break
				}
				highlights = summaryHighlights
				finalMessage = composeFinalMessage(highlights, fallback)
				break
			}
			if len(highlights) == 0 {
				log.Printf("failed to summarize messages: gemini response did not match after %d attempts", maxAttempts)
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

func postGeminiMismatch(session *discordgo.Session, raw string) {
	raw = strings.TrimSpace(raw)
	if session == nil || raw == "" {
		return
	}

	message := fmt.Sprintf("Gemini APIのレスポンスが合わないため再実行\n%s", raw)
	message = truncateRunes(message, messageCharBudget)

	if _, err := session.ChannelMessageSend(geminiMismatchChannelID, message); err != nil {
		log.Printf("failed to post gemini mismatch notice: %v", err)
	}
}
