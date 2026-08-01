package config

import (
	"strings"
	"testing"
	"time"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("YUKAN_SUMMARY_TARGETS", `{"prod":{"guild_id":"g-prod","channel_id":"c-prod"},"dev":{"guild_id":"g-dev","channel_id":"c-dev"}}`)
}

func TestLoadDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Summary.Lookback != 24*time.Hour {
		t.Errorf("Lookback = %v, want 24h", cfg.Summary.Lookback)
	}
	if cfg.Summary.FetchLimit != 100 {
		t.Errorf("FetchLimit = %d, want 100", cfg.Summary.FetchLimit)
	}
	if cfg.Summary.MaxConcurrency != 10 {
		t.Errorf("MaxConcurrency = %d, want 10", cfg.Summary.MaxConcurrency)
	}
	if cfg.DefaultTarget != "prod" {
		t.Errorf("DefaultTarget = %q, want prod", cfg.DefaultTarget)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("GEMINI_API_KEY", "key")
	if _, err := Load(); err == nil {
		t.Error("Load() should fail without DISCORD_BOT_TOKEN")
	}

	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Error("Load() should fail without GEMINI_API_KEY")
	}
}

func TestLoadIntOverrides(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
		check   func(Config) bool
	}{
		{"valid lookback", "YUKAN_SUMMARY_LOOKBACK_HOURS", "48", false, func(c Config) bool { return c.Summary.Lookback == 48*time.Hour }},
		{"valid fetch limit", "YUKAN_SUMMARY_FETCH_LIMIT", "50", false, func(c Config) bool { return c.Summary.FetchLimit == 50 }},
		{"non-integer", "YUKAN_SUMMARY_FETCH_LIMIT", "abc", true, nil},
		{"zero", "YUKAN_SUMMARY_MAX_ATTEMPTS", "0", true, nil},
		{"negative", "YUKAN_SUMMARY_MAX_HIGHLIGHTS", "-3", true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tt.key, tt.value)

			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() should fail for %s=%s", tt.key, tt.value)
				}
				if !strings.Contains(err.Error(), tt.key) {
					t.Errorf("error %q should mention %s", err, tt.key)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !tt.check(cfg) {
				t.Errorf("override %s=%s not applied", tt.key, tt.value)
			}
		})
	}
}

func TestLoadTargetsRequired(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "token")
	t.Setenv("GEMINI_API_KEY", "key")
	t.Setenv("YUKAN_SUMMARY_TARGETS", "")
	if _, err := Load(); err == nil {
		t.Error("Load() should fail without YUKAN_SUMMARY_TARGETS")
	}
}

func TestLoadTargetsOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("YUKAN_SUMMARY_TARGETS", `{"prod":{"guild_id":"g1","channel_id":"c1"}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	target, ok := cfg.Targets["prod"]
	if !ok {
		t.Fatal("prod target missing")
	}
	if target.GuildID != "g1" || target.ChannelID != "c1" {
		t.Errorf("target = %+v", target)
	}
	if target.SourceGuildID != "g1" {
		t.Errorf("SourceGuildID should default to GuildID, got %q", target.SourceGuildID)
	}
}

func TestLoadTargetsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"broken json", `{`},
		{"missing channel_id", `{"prod":{"guild_id":"g1"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("YUKAN_SUMMARY_TARGETS", tt.value)
			if _, err := Load(); err == nil {
				t.Error("Load() should fail")
			}
		})
	}
}

func TestLoadUnknownDefaultTarget(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("YUKAN_DEFAULT_TARGET", "nonexistent")
	if _, err := Load(); err == nil {
		t.Error("Load() should fail when default target is not defined")
	}
}

func TestParsePositiveEnvInt(t *testing.T) {
	t.Setenv("TEST_INT", "")
	if _, set, err := parsePositiveEnvInt("TEST_INT"); set || err != nil {
		t.Errorf("unset should return (_, false, nil), got set=%v err=%v", set, err)
	}

	t.Setenv("TEST_INT", " 42 ")
	v, set, err := parsePositiveEnvInt("TEST_INT")
	if !set || err != nil || v != 42 {
		t.Errorf("got (%d, %v, %v), want (42, true, nil)", v, set, err)
	}
}
