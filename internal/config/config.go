package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the bot.
type Config struct {
	DiscordToken string
	GeminiAPIKey string
	GeminiModel  string

	LogChannelID string

	Summary SummaryConfig

	Targets       map[string]Target
	DefaultTarget string
}

// SummaryConfig controls how summaries are collected and generated.
type SummaryConfig struct {
	Lookback          time.Duration
	FetchLimit        int
	MessageCharBudget int
	MaxHighlights     int
	MaxAttempts       int
	MaxConcurrency    int
}

// Target defines a guild/channel pair for posting summaries.
type Target struct {
	GuildID       string `json:"guild_id"`
	SourceGuildID string `json:"source_guild_id"`
	ChannelID     string `json:"channel_id"`
	LogChannelID  string `json:"log_channel_id"`
}

const (
	defaultLookbackHours        = 24
	defaultFetchLimit           = 100
	defaultMessageCharBudget    = 1800
	defaultMaxHighlights        = 3
	defaultMaxAttempts          = 5
	defaultCollectorConcurrency = 10
)

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		DiscordToken: strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		GeminiAPIKey: strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GeminiModel:  strings.TrimSpace(os.Getenv("GEMINI_MODEL")),
		LogChannelID: strings.TrimSpace(os.Getenv("YUKAN_LOG_CHANNEL_ID")),
		Summary: SummaryConfig{
			Lookback:          time.Duration(defaultLookbackHours) * time.Hour,
			FetchLimit:        defaultFetchLimit,
			MessageCharBudget: defaultMessageCharBudget,
			MaxHighlights:     defaultMaxHighlights,
			MaxAttempts:       defaultMaxAttempts,
			MaxConcurrency:    defaultCollectorConcurrency,
		},
		Targets:       defaultTargets(),
		DefaultTarget: "prod",
	}

	if cfg.DiscordToken == "" {
		return Config{}, fmt.Errorf("DISCORD_BOT_TOKEN is not set")
	}
	if cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	if value := strings.TrimSpace(os.Getenv("YUKAN_DEFAULT_TARGET")); value != "" {
		cfg.DefaultTarget = value
	}

	if lookback := parseEnvInt("YUKAN_SUMMARY_LOOKBACK_HOURS"); lookback > 0 {
		cfg.Summary.Lookback = time.Duration(lookback) * time.Hour
	}
	if fetchLimit := parseEnvInt("YUKAN_SUMMARY_FETCH_LIMIT"); fetchLimit > 0 {
		cfg.Summary.FetchLimit = fetchLimit
	}
	if budget := parseEnvInt("YUKAN_SUMMARY_MESSAGE_BUDGET"); budget > 0 {
		cfg.Summary.MessageCharBudget = budget
	}
	if maxHighlights := parseEnvInt("YUKAN_SUMMARY_MAX_HIGHLIGHTS"); maxHighlights > 0 {
		cfg.Summary.MaxHighlights = maxHighlights
	}
	if attempts := parseEnvInt("YUKAN_SUMMARY_MAX_ATTEMPTS"); attempts > 0 {
		cfg.Summary.MaxAttempts = attempts
	}
	if concurrency := parseEnvInt("YUKAN_SUMMARY_MAX_CONCURRENCY"); concurrency > 0 {
		cfg.Summary.MaxConcurrency = concurrency
	}

	if rawTargets := strings.TrimSpace(os.Getenv("YUKAN_SUMMARY_TARGETS")); rawTargets != "" {
		overrides, err := parseTargets(rawTargets)
		if err != nil {
			return Config{}, err
		}
		if len(overrides) > 0 {
			cfg.Targets = overrides
		}
	}

	if len(cfg.Targets) == 0 {
		return Config{}, fmt.Errorf("no summary targets configured")
	}

	for name, target := range cfg.Targets {
		target.GuildID = strings.TrimSpace(target.GuildID)
		target.SourceGuildID = strings.TrimSpace(target.SourceGuildID)
		target.ChannelID = strings.TrimSpace(target.ChannelID)
		target.LogChannelID = strings.TrimSpace(target.LogChannelID)
		if target.GuildID == "" || target.ChannelID == "" {
			return Config{}, fmt.Errorf("target %q is missing guild_id or channel_id", name)
		}
		if target.SourceGuildID == "" {
			target.SourceGuildID = target.GuildID
		}
		cfg.Targets[name] = target
	}

	if _, ok := cfg.Targets[cfg.DefaultTarget]; !ok {
		return Config{}, fmt.Errorf("default target %q is not defined", cfg.DefaultTarget)
	}

	return cfg, nil
}

func parseEnvInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func parseTargets(raw string) (map[string]Target, error) {
	targets := make(map[string]Target)
	if err := json.Unmarshal([]byte(raw), &targets); err != nil {
		return nil, fmt.Errorf("failed to parse YUKAN_SUMMARY_TARGETS: %w", err)
	}
	for name, target := range targets {
		if strings.TrimSpace(target.GuildID) == "" || strings.TrimSpace(target.ChannelID) == "" {
			return nil, fmt.Errorf("target %q is missing guild_id or channel_id", name)
		}
	}
	return targets, nil
}

func defaultTargets() map[string]Target {
	return map[string]Target{
		"prod": {
			GuildID:       "1239827951667908668",
			SourceGuildID: "1239827951667908668",
			ChannelID:     "1418904371579719710",
			LogChannelID:  "1430142792314650764",
		},
		"dev": {
			GuildID:       "1239827951667908668",
			SourceGuildID: "1239827951667908668",
			ChannelID:     "1417771223433351170",
			LogChannelID:  "1417771223433351170",
		},
	}
}
