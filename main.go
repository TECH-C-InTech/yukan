package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/app"
	"yukan/internal/commands"
	"yukan/internal/gemini"
)

const (
	dailySummaryGuildIDProd   = "1239827951667908668"
	dailySummaryChannelIDProd = "1418904371579719710"
	dailySummaryChannelIDDev  = "1417771223433351170"
)

func main() {
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN is not set")
	}

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		log.Fatal("GEMINI_API_KEY is not set")
	}

	summarizer := gemini.New(geminiKey)
	getCommand := commands.NewGetCommand(summarizer)

	botApp, err := app.New(token, getCommand)
	if err != nil {
		log.Fatalf("failed to initialize bot: %v", err)
	}
	defer botApp.Close()

	if err := botApp.Start(); err != nil {
		log.Fatalf("failed to start bot: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: buildHTTPMux(botApp.Session(), summarizer),
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

func buildHTTPMux(session *discordgo.Session, summarizer commands.Summarizer) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("discord bot running"))
	})

	mux.HandleFunc("/tasks/daily-summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if session == nil {
			http.Error(w, "discord session not ready", http.StatusServiceUnavailable)
			return
		}

		guildID := dailySummaryGuildIDProd
		channelID := dailySummaryChannelIDProd
		switch strings.ToLower(r.URL.Query().Get("target")) {
		case "dev":
			guildID = dailySummaryGuildIDProd
			channelID = dailySummaryChannelIDDev
		}

		ctx := r.Context()
		message, highlights, digests, err := commands.GenerateSummaryForGuild(ctx, session, summarizer, guildID)
		if err != nil {
			log.Printf("daily summary: failed to generate message: %v", err)
			http.Error(w, "failed to generate summary", http.StatusInternalServerError)
			return
		}

		trimmed := strings.TrimSpace(message)
		if trimmed == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		content, embeds := commands.BuildSummaryMessage(trimmed, highlights, digests)
		if len(embeds) > 0 {
			if content != "" {
				if _, err := session.ChannelMessageSend(channelID, content); err != nil {
					log.Printf("daily summary: failed to post header message: %v", err)
					http.Error(w, "failed to post summary", http.StatusInternalServerError)
					return
				}
			}
			for _, embed := range embeds {
				if embed == nil {
					continue
				}
				if _, err := session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}}); err != nil {
					log.Printf("daily summary: failed to post embed message: %v", err)
					http.Error(w, "failed to post summary", http.StatusInternalServerError)
					return
				}
			}
		} else {
			if _, err := session.ChannelMessageSend(channelID, trimmed); err != nil {
				log.Printf("daily summary: failed to post message: %v", err)
				http.Error(w, "failed to post summary", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("posted"))
	})

	return mux
}
