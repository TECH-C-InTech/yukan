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

const geminiMismatchChannelID = "1430142792314650764"

// Summarizer generates structured highlights from the collected messages.
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) ([]Highlight, error)
}

type summaryContextKey string

const summaryStartKey summaryContextKey = "commands-summary-start"

// WithSummaryStart stores the workflow trigger timestamp in the provided context.
func WithSummaryStart(ctx context.Context, start time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, summaryStartKey, start)
}

// GenerateSummaryForGuild collects the latest messages and produces a final digest message.
func GenerateSummaryForGuild(ctx context.Context, session *discordgo.Session, summarizer Summarizer, guildID string) (string, []Highlight, []channelDigest, error) {
	if session == nil {
		return "", nil, nil, fmt.Errorf("session is nil")
	}

	workflowStart := time.Now()
	if ctx != nil {
		if value := ctx.Value(summaryStartKey); value != nil {
			if ts, ok := value.(time.Time); ok && !ts.IsZero() {
				workflowStart = ts
			}
		}
	}
	postGeminiLogMessage(session, workflowStart.Format("2006-01-02"))
	postGeminiLogMessage(session, fmt.Sprintf("トリガー開始 (累計%s)", formatDuration(time.Since(workflowStart))))
	postGeminiLogMessage(session, fmt.Sprintf("メッセージ取得開始 (累計%s)", formatDuration(time.Since(workflowStart))))
	fetchStart := time.Now()

	botID := ""
	if session.State != nil && session.State.User != nil {
		botID = session.State.User.ID
	}

	digests, fallback, err := collectLatestMessages(session, guildID, botID)
	if err != nil {
		postGeminiFailureMessage(session, fmt.Sprintf("メッセージ取得失敗 (所要%s)\n%v", formatDuration(time.Since(fetchStart)), err))
		return "", nil, nil, err
	}
	totalMessages := countMessages(digests)
	postGeminiLogMessage(session, fmt.Sprintf("メッセージ取得終了 (所要%s, 累計%s, チャンネル=%d, メッセージ=%d)", formatDuration(time.Since(fetchStart)), formatDuration(time.Since(workflowStart)), len(digests), totalMessages))

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
		if prompt == "" {
			log.Printf("generate summary: prompt is empty, using fallback message")
			postGeminiLogMessage(session, fmt.Sprintf("Gemini呼び出しスキップ (プロンプトなし, 累計%s)", formatDuration(time.Since(workflowStart))))
		} else {
			const maxAttempts = 5
			var lastRawResponse string
			for attempts := 0; attempts < maxAttempts && summaryCtx.Err() == nil; attempts++ {
				attemptNum := attempts + 1
				log.Printf("generate summary: attempt %d/%d (prompt length=%d)", attemptNum, maxAttempts, len(prompt))
				postGeminiLogMessage(session, fmt.Sprintf("Gemini呼び出し開始 (%d/%d, 累計%s)", attemptNum, maxAttempts, formatDuration(time.Since(workflowStart))))
				geminiStart := time.Now()
				summaryHighlights, err := summarizer.Summarize(summaryCtx, prompt)
				postGeminiLogMessage(session, fmt.Sprintf("Gemini呼び出し終了 (%d/%d, 所要%s, 累計%s)", attemptNum, maxAttempts, formatDuration(time.Since(geminiStart)), formatDuration(time.Since(workflowStart))))
				if err != nil {
					var rawErr interface{ RawResponse() string }
					if errors.As(err, &rawErr) && rawErr != nil {
						raw := strings.TrimSpace(rawErr.RawResponse())
						if raw != "" {
							lastRawResponse = raw
							postGeminiFailureMessage(session, fmt.Sprintf("Geminiハイライト生成が失敗しました (%d/%d)\n%s", attemptNum, maxAttempts, raw))
						} else {
							postGeminiFailureMessage(session, fmt.Sprintf("Geminiハイライト生成が失敗しました (%d/%d)", attemptNum, maxAttempts))
						}
						continue
					}
					log.Printf("failed to summarize messages: %v", err)
					lastRawResponse = fmt.Sprintf("%v", err)
					postGeminiFailureMessage(session, fmt.Sprintf("Geminiハイライト生成が失敗しました (%d/%d)\n%v", attemptNum, maxAttempts, err))
					continue
				}
				if len(summaryHighlights) == 0 {
					log.Printf("failed to summarize messages: gemini returned no highlights (attempt %d/%d)", attemptNum, maxAttempts)
					postGeminiFailureMessage(session, fmt.Sprintf("Geminiが空のハイライトを返したため再試行します (%d/%d)", attemptNum, maxAttempts))
					continue
				}
				highlights = summaryHighlights
				finalMessage = composeFinalMessage(highlights, fallback)
				log.Printf("generate summary: attempt %d/%d succeeded with %d highlights", attemptNum, maxAttempts, len(highlights))
				break
			}
			if len(highlights) == 0 {
				log.Printf("failed to summarize messages: gemini response did not match after %d attempts", maxAttempts)
				message := fmt.Sprintf("Geminiハイライト生成が%d回失敗しました", maxAttempts)
				if raw := strings.TrimSpace(lastRawResponse); raw != "" {
					message = fmt.Sprintf("%s\n%s", message, raw)
				}
				postGeminiFailureMessage(session, message)
			}
		}
	} else {
		postGeminiLogMessage(session, fmt.Sprintf("Gemini呼び出しスキップ (メッセージ=%d, 累計%s)", totalMessages, formatDuration(time.Since(workflowStart))))
	}

	postGeminiLogMessage(session, fmt.Sprintf("結果生成完了 (ハイライト=%d, 累計%s)", len(highlights), formatDuration(time.Since(workflowStart))))

	if strings.TrimSpace(finalMessage) == "" {
		finalMessage = fallback
	}

	if utf8.RuneCountInString(finalMessage) > messageCharBudget {
		finalMessage = truncateRunes(finalMessage, messageCharBudget)
	}
	postGeminiLogMessage(session, fmt.Sprintf("出力準備完了 (文字数=%d, 累計%s)", utf8.RuneCountInString(finalMessage), formatDuration(time.Since(workflowStart))))

	return finalMessage, highlights, digests, nil
}

func postGeminiFailureMessage(session *discordgo.Session, message string) {
	postGeminiChannelMessage(session, "gemini summary failure", message)
}

func postGeminiLogMessage(session *discordgo.Session, message string) {
	postGeminiChannelMessage(session, "gemini summary log", message)
}

func postGeminiChannelMessage(session *discordgo.Session, prefix, message string) {
	if session == nil {
		return
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	message = truncateRunes(message, messageCharBudget)

	if prefix == "" {
		prefix = "gemini summary"
	}
	log.Printf("%s: %s", prefix, message)

	if _, err := session.ChannelMessageSend(geminiMismatchChannelID, message); err != nil {
		log.Printf("failed to post gemini mismatch notice: %v", err)
	}
}

// NotifySummaryFailure exposes failure logging for callers outside this package.
func NotifySummaryFailure(session *discordgo.Session, message string) {
	postGeminiFailureMessage(session, message)
}

// NotifySummaryLog exposes informational logging for callers outside this package.
func NotifySummaryLog(session *discordgo.Session, message string) {
	postGeminiLogMessage(session, message)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func countMessages(digests []channelDigest) int {
	total := 0
	for _, digest := range digests {
		total += len(digest.Messages)
	}
	return total
}
