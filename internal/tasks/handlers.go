// Package tasks は Cloud Scheduler から起動される HTTP エンドポイントを提供する。
package tasks

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/config"
	"yukan/internal/notifier"
	"yukan/internal/summary"
)

// Config wires the HTTP handlers.
type Config struct {
	Session        *discordgo.Session
	SummaryService *summary.Service
	Notifier       notifier.Notifier
	Targets        map[string]config.Target
	DefaultTarget  string
}

// NewMux builds the HTTP routing for cron tasks.
func NewMux(cfg Config) http.Handler {
	handler := &Handler{
		session:       cfg.Session,
		service:       cfg.SummaryService,
		notifier:      cfg.Notifier,
		targets:       cfg.Targets,
		defaultTarget: cfg.DefaultTarget,
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
	service       *summary.Service
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
	if h.service == nil || h.session == nil {
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

	botID := ""
	if h.session.State != nil && h.session.State.User != nil {
		botID = h.session.State.User.ID
	}

	sourceGuildID := target.SourceGuildID
	if sourceGuildID == "" {
		sourceGuildID = target.GuildID
	}

	ctx := summary.WithSummaryStart(r.Context(), time.Now())
	ctx = notifier.WithTarget(ctx, targetName)

	result, err := h.service.Generate(ctx, summary.Request{
		TargetName: targetName,
		GuildID:    sourceGuildID,
		ChannelID:  target.ChannelID,
		BotID:      botID,
	})
	if err != nil {
		log.Printf("daily summary: failed to generate message: %v", err)
		http.Error(w, "failed to generate summary", http.StatusInternalServerError)
		return
	}

	trimmed := strings.TrimSpace(result.Content)
	if trimmed == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !result.Publishable {
		if h.notifier != nil {
			h.notifier.Error(ctx, fmt.Sprintf("夕刊ポストをスキップしました (target=%s channel=%s)", targetName, target.ChannelID))
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if trimmed != "" {
		if _, err := h.session.ChannelMessageSend(target.ChannelID, trimmed); err != nil {
			log.Printf("daily summary: failed to post header message: %v", err)
			http.Error(w, "failed to post summary", http.StatusInternalServerError)
			return
		}
	}

	for _, embed := range result.Embeds {
		if embed == nil {
			continue
		}
		if _, err := h.session.ChannelMessageSendComplex(target.ChannelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}}); err != nil {
			log.Printf("daily summary: failed to post embed message: %v", err)
			http.Error(w, "failed to post summary", http.StatusInternalServerError)
			return
		}
	}

	if h.notifier != nil {
		elapsed := time.Since(result.WorkflowStart).Seconds()
		h.notifier.Info(ctx, fmt.Sprintf("出力送信完了 (target=%s channel=%s, 累計%.2fs)", targetName, target.ChannelID, elapsed))
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("posted"))
}
