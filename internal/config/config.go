// Package config は環境変数からボットの実行時設定を読み込み検証する。
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

	intOverrides := []struct {
		key   string
		apply func(int)
	}{
		{"YUKAN_SUMMARY_LOOKBACK_HOURS", func(v int) { cfg.Summary.Lookback = time.Duration(v) * time.Hour }},
		{"YUKAN_SUMMARY_FETCH_LIMIT", func(v int) { cfg.Summary.FetchLimit = v }},
		{"YUKAN_SUMMARY_MESSAGE_BUDGET", func(v int) { cfg.Summary.MessageCharBudget = v }},
		{"YUKAN_SUMMARY_MAX_HIGHLIGHTS", func(v int) { cfg.Summary.MaxHighlights = v }},
		{"YUKAN_SUMMARY_MAX_ATTEMPTS", func(v int) { cfg.Summary.MaxAttempts = v }},
		{"YUKAN_SUMMARY_MAX_CONCURRENCY", func(v int) { cfg.Summary.MaxConcurrency = v }},
	}
	for _, o := range intOverrides {
		value, set, err := parsePositiveEnvInt(o.key)
		if err != nil {
			return Config{}, err
		}
		if set {
			o.apply(value)
		}
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

// parsePositiveEnvInt は環境変数を正の整数として読む。未設定は (0, false, nil)。
// 整数でない・0 以下の値は設定ミスとしてエラーを返す (以前は無言でデフォルトに落ちていた)。
func parsePositiveEnvInt(key string) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be an integer, got %q", key, raw)
	}
	if value <= 0 {
		return 0, false, fmt.Errorf("%s must be positive, got %d", key, value)
	}
	return value, true, nil
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
