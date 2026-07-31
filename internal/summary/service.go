// Package summary は Discord メッセージの収集・要約・夕刊メッセージの組み立てを担う。
package summary

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"yukan/internal/notifier"
)

type summaryContextKey string

const summaryStartKey summaryContextKey = "summary-start"

var (
	infoRand   = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // 演出用の乱数で暗号用途ではない
	infoRandMu sync.Mutex
)

// WithSummaryStart stores the workflow trigger timestamp in the provided context.
func WithSummaryStart(ctx context.Context, start time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, summaryStartKey, start)
}

func workflowStartFromContext(ctx context.Context) time.Time {
	if ctx == nil {
		return time.Now()
	}
	if value := ctx.Value(summaryStartKey); value != nil {
		if ts, ok := value.(time.Time); ok && !ts.IsZero() {
			return ts
		}
	}
	return time.Now()
}

// Service coordinates collectors, LLMs, and presenters to build summaries.
type Service struct {
	Collector  *Collector
	Summarizer Summarizer
	Notifier   notifier.Notifier
	Config     Config
}

// Generate produces a summary result for the target guild.
func (s *Service) Generate(ctx context.Context, req Request) (Result, error) {
	result := Result{WorkflowStart: workflowStartFromContext(ctx)}

	ctx = notifier.WithTarget(ctx, req.TargetName)

	if req.GuildID == "" {
		return result, fmt.Errorf("guild id is required")
	}
	if s == nil || s.Collector == nil {
		return result, fmt.Errorf("collector is not configured")
	}

	workflowStart := result.WorkflowStart
	s.info(ctx, fmt.Sprintf("トリガー開始 (target=%s)", req.TargetName))
	s.info(ctx, "メッセージ取得開始")

	collectorResult, err := s.Collector.Collect(ctx, req.GuildID, req.BotID)
	if err != nil {
		s.error(ctx, fmt.Sprintf("メッセージ取得失敗 (%s)", err))
		return result, err
	}

	result.Digests = collectorResult.Digests
	result.Fallback = collectorResult.FallbackText
	result.TotalMessages = collectorResult.TotalMessages

	s.info(ctx, fmt.Sprintf("メッセージ取得終了 (チャンネル=%d, メッセージ=%d, 所要%s)", len(collectorResult.Digests), collectorResult.TotalMessages, formatDuration(time.Since(workflowStart))))

	finalMessage := collectorResult.FallbackText
	var highlights []Highlight

	if len(collectorResult.Digests) > 0 && s.Summarizer != nil {
		summaryCtx := ctx
		if summaryCtx == nil {
			summaryCtx = context.Background()
		}

		prompt := buildSummaryPrompt(collectorResult.Digests, s.maxHighlights())
		if prompt == "" {
			s.info(ctx, "Gemini呼び出しスキップ (プロンプトなし)")
		} else {
			maxAttempts := s.maxAttempts()
			var lastRawResponse string
			for attempt := 1; attempt <= maxAttempts && summaryCtx.Err() == nil; attempt++ {
				s.info(ctx, fmt.Sprintf("Gemini呼び出し開始 (%d/%d)", attempt, maxAttempts))
				geminiStart := time.Now()
				summaryHighlights, err := s.Summarizer.Summarize(summaryCtx, prompt)
				s.info(ctx, fmt.Sprintf("Gemini呼び出し終了 (%d/%d, 所要%s)", attempt, maxAttempts, formatDuration(time.Since(geminiStart))))
				if err != nil {
					rawMsg := extractRawResponse(err)
					if rawMsg != "" {
						lastRawResponse = rawMsg
					} else {
						lastRawResponse = err.Error()
					}
					s.error(ctx, fmt.Sprintf("Geminiハイライト生成が失敗しました (%d/%d)\n%s", attempt, maxAttempts, lastRawResponse))
					continue
				}
				if len(summaryHighlights) == 0 {
					s.error(ctx, fmt.Sprintf("Geminiが空のハイライトを返したため再試行します (%d/%d)", attempt, maxAttempts))
					continue
				}
				highlights = summaryHighlights
				finalMessage = composeFinalMessage(highlights, collectorResult.FallbackText, s.charBudget())
				log.Printf("summary: Gemini attempt %d/%d succeeded with %d highlights", attempt, maxAttempts, len(highlights))
				break
			}
			if len(highlights) == 0 {
				message := fmt.Sprintf("Geminiハイライト生成が%d回失敗しました", maxAttempts)
				if raw := strings.TrimSpace(lastRawResponse); raw != "" {
					message = fmt.Sprintf("%s\n%s", message, raw)
				}
				s.error(ctx, message)
			}
		}
	} else {
		s.info(ctx, fmt.Sprintf("Gemini呼び出しスキップ (メッセージ=%d)", collectorResult.TotalMessages))
	}

	s.info(ctx, fmt.Sprintf("結果生成完了 (ハイライト=%d)", len(highlights)))

	finalMessage = strings.TrimSpace(finalMessage)
	if finalMessage == "" {
		finalMessage = strings.TrimSpace(collectorResult.FallbackText)
	}
	if finalMessage != "" {
		finalMessage = truncateRunes(finalMessage, s.charBudget())
	}

	content, embeds := BuildSummaryMessage(finalMessage, highlights, collectorResult.Digests)

	result.Content = content
	result.Embeds = embeds
	result.Highlights = highlights
	result.Publishable = len(embeds) > 0
	result.CompletedAt = time.Now()

	s.info(ctx, fmt.Sprintf("出力準備完了 (文字数=%d, 累計%s)", utf8.RuneCountInString(result.Content), formatDuration(time.Since(workflowStart))))

	return result, nil
}

func (s *Service) charBudget() int {
	if s == nil || s.Config.MessageCharBudget <= 0 {
		return 1800
	}
	return s.Config.MessageCharBudget
}

func (s *Service) maxHighlights() int {
	if s == nil || s.Config.MaxHighlights <= 0 {
		return 3
	}
	return s.Config.MaxHighlights
}

func (s *Service) maxAttempts() int {
	if s == nil || s.Config.MaxAttempts <= 0 {
		return 5
	}
	return s.Config.MaxAttempts
}

func (s *Service) info(ctx context.Context, message string) {
	if s != nil && s.Notifier != nil {
		s.Notifier.Info(ctx, message)

		infoRandMu.Lock()
		isOddity := infoRand.Intn(20) == 0
		infoRandMu.Unlock()

		if isOddity {
			s.Notifier.Info(ctx, "👁️👁️")
		}
	} else {
		log.Printf("summary info: %s", message)
	}
}

func (s *Service) error(ctx context.Context, message string) {
	if s != nil && s.Notifier != nil {
		s.Notifier.Error(ctx, message)
	} else {
		log.Printf("summary error: %s", message)
	}
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func extractRawResponse(err error) string {
	if err == nil {
		return ""
	}
	var rawErr interface{ RawResponse() string }
	if errors.As(err, &rawErr) && rawErr != nil {
		return strings.TrimSpace(rawErr.RawResponse())
	}
	return ""
}
