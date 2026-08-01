package summary

import (
	"strings"
	"testing"
	"time"
)

func TestBuildSummaryPrompt(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	digests := []ChannelDigest{{
		Name: "general",
		Messages: []MessageDigest{
			// Collector は新しい順に積むため、プロンプトでは逆順 (古い順) に並ぶ
			{Author: "b", Content: "あとの発言", Timestamp: base.Add(time.Hour), Link: "https://discord.com/channels/1/2/4"},
			{Author: "a", Content: "さきの発言", Timestamp: base, Link: "https://discord.com/channels/1/2/3"},
		},
	}}

	prompt := buildSummaryPrompt(digests, 3, 24*time.Hour)

	if !strings.Contains(prompt, "最大3件") {
		t.Errorf("prompt should mention max highlights: %q", prompt)
	}
	if !strings.Contains(prompt, "過去24時間") {
		t.Errorf("prompt should mention lookback hours")
	}
	if !strings.Contains(prompt, "# general") {
		t.Errorf("prompt should contain channel header")
	}
	first := strings.Index(prompt, "さきの発言")
	second := strings.Index(prompt, "あとの発言")
	if first == -1 || second == -1 || first > second {
		t.Errorf("messages should appear oldest-first (first=%d, second=%d)", first, second)
	}
	if !strings.Contains(prompt, "https://discord.com/channels/1/2/3") {
		t.Errorf("prompt should include message links")
	}
}

func TestBuildSummaryPromptEdgeCases(t *testing.T) {
	if got := buildSummaryPrompt(nil, 3, 24*time.Hour); got != "" {
		t.Errorf("empty digests should return empty prompt, got %q", got)
	}
	digests := []ChannelDigest{{Name: "c", Messages: []MessageDigest{{Author: "a", Content: "x"}}}}
	if got := buildSummaryPrompt(digests, 0, 0); !strings.Contains(got, "最大3件") {
		t.Errorf("non-positive maxHighlights should fall back to 3")
	}
}
