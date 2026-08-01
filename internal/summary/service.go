// Package summary は Discord メッセージの収集・要約・夕刊メッセージの組み立てを担う。
package summary

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/notifier"
)

type summaryContextKey string

const summaryStartKey summaryContextKey = "summary-start"

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

// MessageCollector は要約対象メッセージの収集を抽象化する (テストでフェイクを注入する)。
type MessageCollector interface {
	Collect(ctx context.Context, guildID, botID string) (CollectorResult, error)
}

// Publisher は生成した夕刊の投稿を抽象化する。
type Publisher interface {
	Publish(ctx context.Context, channelID, content string, embeds []*discordgo.MessageEmbed) error
}

// Service coordinates collectors, LLMs, and presenters to build summaries.
type Service struct {
	Collector  MessageCollector
	Summarizer Summarizer
	Publisher  Publisher
	Notifier   notifier.Notifier
	Config     Config

	// Oddity は info 通知に 👁️👁️ を添えるかを決める。nil なら 1/20 の乱数
	Oddity func() bool

	// Backoff はリトライ間隔を決める。nil なら指数バックオフ (テストでゼロを注入する)
	Backoff func(attempt int) time.Duration
}

// Run は夕刊を生成し、公開可能なら req.ChannelID へ投稿まで行う。
func (s *Service) Run(ctx context.Context, req Request) (Result, error) {
	result, err := s.Generate(ctx, req)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(result.Content) == "" {
		return result, nil
	}
	if !result.Publishable {
		s.error(ctx, fmt.Sprintf("夕刊ポストをスキップしました (target=%s channel=%s)", req.TargetName, req.ChannelID))
		return result, nil
	}
	if s.Publisher == nil {
		return result, fmt.Errorf("publisher is not configured")
	}
	if err := s.Publisher.Publish(ctx, req.ChannelID, strings.TrimSpace(result.Content), result.Embeds); err != nil {
		s.error(ctx, fmt.Sprintf("夕刊の投稿に失敗しました (%s)", err))
		return result, err
	}
	result.Posted = true
	s.info(ctx, fmt.Sprintf("出力送信完了 (target=%s channel=%s, 累計%s)", req.TargetName, req.ChannelID, formatDuration(time.Since(result.WorkflowStart))))
	return result, nil
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
	if collectorResult.SkippedChannels > 0 {
		s.info(ctx, fmt.Sprintf("取得できないチャンネルを%d件スキップしました", collectorResult.SkippedChannels))
	}

	finalMessage := collectorResult.FallbackText
	var highlights []Highlight

	if len(collectorResult.Digests) > 0 && s.Summarizer != nil {
		summaryCtx := ctx
		if summaryCtx == nil {
			summaryCtx = context.Background()
		}

		prompt := buildSummaryPrompt(collectorResult.Digests, s.Config.MaxHighlights)
		if prompt == "" {
			s.info(ctx, "Gemini呼び出しスキップ (プロンプトなし)")
		} else {
			maxAttempts := s.Config.MaxAttempts
			var lastRawResponse string
			for attempt := 1; attempt <= maxAttempts && summaryCtx.Err() == nil; attempt++ {
				if attempt > 1 {
					backoff := s.Backoff
					if backoff == nil {
						backoff = retryBackoff
					}
					select {
					case <-time.After(backoff(attempt)):
					case <-summaryCtx.Done():
						continue
					}
				}
				s.info(ctx, fmt.Sprintf("Gemini呼び出し開始 (%d/%d)", attempt, maxAttempts))
				geminiStart := time.Now()
				raw, err := s.Summarizer.Summarize(summaryCtx, prompt)
				s.info(ctx, fmt.Sprintf("Gemini呼び出し終了 (%d/%d, 所要%s)", attempt, maxAttempts, formatDuration(time.Since(geminiStart))))
				if err != nil {
					lastRawResponse = err.Error()
					s.error(ctx, fmt.Sprintf("Geminiハイライト生成が失敗しました (%d/%d)\n%s", attempt, maxAttempts, lastRawResponse))
					continue
				}
				summaryHighlights, err := ParseHighlights(raw)
				if err != nil {
					lastRawResponse = raw
					s.error(ctx, fmt.Sprintf("Geminiハイライトの解析に失敗したため再試行します (%d/%d)\n%s", attempt, maxAttempts, raw))
					continue
				}
				highlights = summaryHighlights
				finalMessage = composeFinalMessage(highlights, collectorResult.FallbackText, s.Config.MessageCharBudget)
				slog.InfoContext(ctx, "gemini attempt succeeded", "attempt", attempt, "max_attempts", maxAttempts, "highlights", len(highlights))
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
		finalMessage = truncateRunes(finalMessage, s.Config.MessageCharBudget)
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

// info はログ (Cloud Logging) と運用者向け Discord 通知の両方に流す。
func (s *Service) info(ctx context.Context, message string) {
	slog.InfoContext(ctx, message)
	if s != nil && s.Notifier != nil {
		s.Notifier.Info(ctx, message)

		oddity := s.Oddity
		if oddity == nil {
			oddity = defaultOddity
		}
		if oddity() {
			s.Notifier.Info(ctx, "👁️👁️")
		}
	}
}

// retryBackoff は 2 回目以降のリトライ間隔 (指数バックオフ + ジッター) を返す。
func retryBackoff(attempt int) time.Duration {
	base := 2 * time.Second << (attempt - 2) // 2s, 4s, 8s, ...
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	jitter := time.Duration(rand.Int64N(int64(base / 4))) //nolint:gosec // リトライ分散用の乱数で暗号用途ではない
	return base + jitter
}

func defaultOddity() bool {
	return rand.IntN(20) == 0 //nolint:gosec // 演出用の乱数で暗号用途ではない
}

func (s *Service) error(ctx context.Context, message string) {
	slog.ErrorContext(ctx, message)
	if s != nil && s.Notifier != nil {
		s.Notifier.Error(ctx, message)
	}
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
