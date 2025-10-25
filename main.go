package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		log.Fatalf("failed to create Discord session: %v", err)
	}
	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages

	if err := session.Open(); err != nil {
		log.Fatalf("failed to start Discord session: %v", err)
	}
	defer session.Close()

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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	log.Println("Bot is running. Press Ctrl+C to exit.")

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
