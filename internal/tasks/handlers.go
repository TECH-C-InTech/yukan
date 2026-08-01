// Package tasks は Cloud Scheduler から起動される HTTP エンドポイントを提供する。
package tasks

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/config"
	"yukan/internal/notifier"
	"yukan/internal/summary"
)

// summaryRunner は夕刊の生成 + 投稿を抽象化する (テストでフェイクを注入する)。
type summaryRunner interface {
	Run(ctx context.Context, req summary.Request) (summary.Result, error)
}

// Config wires the HTTP handlers.
type Config struct {
	Session        *discordgo.Session
	BotID          string
	SummaryService *summary.Service
	Notifier       notifier.Notifier
	Targets        map[string]config.Target
	DefaultTarget  string
}

// NewMux builds the HTTP routing for cron tasks.
func NewMux(cfg Config) http.Handler {
	handler := &Handler{
		session:       cfg.Session,
		botID:         cfg.BotID,
		notifier:      cfg.Notifier,
		targets:       cfg.Targets,
		defaultTarget: cfg.DefaultTarget,
	}
	// typed nil をインターフェースに入れると nil チェックが効かなくなるため明示的に分岐する
	if cfg.SummaryService != nil {
		handler.service = cfg.SummaryService
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handler.handleHealthz)
	mux.HandleFunc("/", handler.handleRoot)
	mux.HandleFunc("/tasks/daily-summary", handler.handleDailySummary)

	return mux
}

// Handler processes task invocations.
type Handler struct {
	session       *discordgo.Session
	botID         string
	service       summaryRunner
	notifier      notifier.Notifier
	targets       map[string]config.Target
	defaultTarget string
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) handleRoot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("discord bot running"))
}

func (h *Handler) handleDailySummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.service == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}

	targetName := strings.TrimSpace(r.URL.Query().Get("target"))
	if targetName == "" {
		targetName = h.defaultTarget
	}
	target, ok := h.targets[targetName]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown target %q", targetName), http.StatusBadRequest)
		return
	}
	if target.GuildID == "" || target.ChannelID == "" {
		http.Error(w, fmt.Sprintf("target %q is misconfigured", targetName), http.StatusInternalServerError)
		return
	}

	botID := h.botID
	if botID == "" && h.session != nil && h.session.State != nil && h.session.State.User != nil {
		botID = h.session.State.User.ID
	}

	sourceGuildID := target.SourceGuildID
	if sourceGuildID == "" {
		sourceGuildID = target.GuildID
	}

	ctx := summary.WithSummaryStart(r.Context(), time.Now())
	ctx = notifier.WithTarget(ctx, targetName)

	result, err := h.service.Run(ctx, summary.Request{
		TargetName: targetName,
		GuildID:    sourceGuildID,
		ChannelID:  target.ChannelID,
		BotID:      botID,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "daily summary failed", "target", targetName, "error", err)
		http.Error(w, "failed to run summary", http.StatusInternalServerError)
		return
	}

	if !result.Posted {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("posted"))
}
