// yukan は Discord サーバーの1日を Gemini で要約して夕刊として投稿するボット。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/config"
	"yukan/internal/gemini"
	"yukan/internal/notifier"
	"yukan/internal/summary"
	"yukan/internal/tasks"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Cloud Run: ポートを即座に listen するため、サーバーを最優先で起動
	var realHandler http.Handler
	var handlerMu sync.RWMutex
	var ready bool

	healthz := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	proxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			healthz.ServeHTTP(w, r)
			return
		}
		handlerMu.RLock()
		h := realHandler
		ok := ready
		handlerMu.RUnlock()
		if !ok || h == nil {
			http.Error(w, "service initializing", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on :%s", port) //nolint:gosec // PORT は運用者が設定する環境変数で外部入力ではない
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// 初期化
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config load failed: %v (returning 503)", err)
	} else {
		session, err := discordgo.New("Bot " + cfg.DiscordToken)
		if err != nil {
			log.Printf("failed to create Discord session: %v", err)
		} else {
			session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages
			if err := session.Open(); err != nil {
				log.Printf("failed to start Discord session: %v", err)
			} else {
				defer func() { _ = session.Close() }()
				geminiClient := gemini.New(cfg.GeminiAPIKey)
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
				collector := &summary.Collector{
					Session:        session,
					Lookback:       cfg.Summary.Lookback,
					FetchLimit:     cfg.Summary.FetchLimit,
					MaxConcurrency: cfg.Summary.MaxConcurrency,
				}
				service := &summary.Service{
					Collector:  collector,
					Summarizer: geminiClient,
					Notifier:   note,
					Config: summary.Config{
						MessageCharBudget: cfg.Summary.MessageCharBudget,
						MaxHighlights:     cfg.Summary.MaxHighlights,
						MaxAttempts:       cfg.Summary.MaxAttempts,
					},
				}
				mux := tasks.NewMux(tasks.Config{
					Session:        session,
					SummaryService: service,
					Notifier:       note,
					Targets:        cfg.Targets,
					DefaultTarget:  cfg.DefaultTarget,
				})
				handlerMu.Lock()
				realHandler = mux
				ready = true
				handlerMu.Unlock()
				log.Println("Bot is running.")
			}
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case <-stop:
		log.Println("shutdown signal received")
	case err := <-serverErrors:
		log.Printf("HTTP server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("failed to shut down HTTP server: %v", err)
	}

	log.Println("Shutting down bot")
}
