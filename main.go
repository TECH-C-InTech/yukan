// yukan は Discord サーバーの1日を Gemini で要約して夕刊として投稿するボット。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/config"
	"yukan/internal/discord"
	"yukan/internal/gemini"
	"yukan/internal/logging"
	"yukan/internal/notifier"
	"yukan/internal/summary"
	"yukan/internal/tasks"
)

func main() {
	slog.SetDefault(slog.New(logging.NewHandler()))
	if err := run(); err != nil {
		// 初期化失敗は即終了する。Cloud Run 側でリビジョンが unhealthy になり、
		// 壊れたデプロイが「健康な503」に見える事故を防ぐ
		log.Fatalf("startup failed: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}

	// gateway (WebSocket) は開かず REST のみ使用する。起動が即時になり、
	// スケールゼロ運用と噛み合う
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return fmt.Errorf("failed to create Discord session: %w", err)
	}

	botID, err := discord.BotUserID(session)
	if err != nil {
		return err
	}

	geminiClient, err := gemini.New(context.Background(), cfg.GeminiAPIKey)
	if err != nil {
		return err
	}
	if cfg.GeminiModel != "" {
		geminiClient = geminiClient.WithModel(cfg.GeminiModel)
	}

	targetLogChannels := make(map[string]string, len(cfg.Targets))
	for name, target := range cfg.Targets {
		if target.LogChannelID != "" {
			targetLogChannels[name] = target.LogChannelID
		}
	}
	note := &notifier.DiscordNotifier{
		Session:          session,
		ChannelID:        cfg.LogChannelID,
		MaxMessageLength: cfg.Summary.MessageCharBudget,
		TargetChannels:   targetLogChannels,
	}

	service := &summary.Service{
		Collector: &summary.Collector{
			Session:        session,
			Lookback:       cfg.Summary.Lookback,
			FetchLimit:     cfg.Summary.FetchLimit,
			MaxConcurrency: cfg.Summary.MaxConcurrency,
		},
		Summarizer: geminiClient,
		Publisher:  &discord.Publisher{Session: session},
		Notifier:   note,
		Config: summary.Config{
			MessageCharBudget: cfg.Summary.MessageCharBudget,
			MaxHighlights:     cfg.Summary.MaxHighlights,
			MaxAttempts:       cfg.Summary.MaxAttempts,
			Lookback:          cfg.Summary.Lookback,
		},
	}

	mux := tasks.NewMux(tasks.Config{
		Session:        session,
		BotID:          botID,
		SummaryService: service,
		Notifier:       note,
		Targets:        cfg.Targets,
		DefaultTarget:  cfg.DefaultTarget,
	})

	// Cloud Run 上ではリクエストのトレース ID をログに載せる
	handler := logging.HTTPMiddleware(os.Getenv("GOOGLE_CLOUD_PROJECT"), mux)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout は設定しない: 要約は数分かかり、
		// 上限は Cloud Run の timeout と Scheduler の attempt_deadline が担保する
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return err
	case <-stop:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}
	return nil
}
