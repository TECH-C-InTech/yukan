package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestSanitizeContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "hello", "hello"},
		{"newlines", "a\nb\r\nc", "a b  c"},
		{"whitespace only", "  \n ", "(内容なし)"},
		{"empty", "", "(内容なし)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeContent(tt.input); got != tt.want {
				t.Errorf("sanitizeContent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildMessageLink(t *testing.T) {
	if got := buildMessageLink("g", "c", "m"); got != "https://discord.com/channels/g/c/m" {
		t.Errorf("buildMessageLink = %q", got)
	}
	for _, args := range [][3]string{{"", "c", "m"}, {"g", "", "m"}, {"g", "c", ""}} {
		if got := buildMessageLink(args[0], args[1], args[2]); got != "" {
			t.Errorf("buildMessageLink(%v) = %q, want empty", args, got)
		}
	}
}

func TestDisplayNameFromUser(t *testing.T) {
	tests := []struct {
		name string
		user *discordgo.User
		want string
	}{
		{"nil", nil, ""},
		{"global name wins", &discordgo.User{GlobalName: "グローバル", Username: "user1"}, "グローバル"},
		{"username without discriminator", &discordgo.User{Username: "user1", Discriminator: "0"}, "user1"},
		{"legacy discriminator", &discordgo.User{Username: "user1", Discriminator: "1234"}, "user1#1234"},
		{"empty", &discordgo.User{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayNameFromUser(tt.user); got != tt.want {
				t.Errorf("displayNameFromUser = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayNameFromMember(t *testing.T) {
	member := &discordgo.Member{Nick: "ニック", User: &discordgo.User{GlobalName: "グローバル"}}
	if got := displayNameFromMember(member); got != "ニック" {
		t.Errorf("nick should win, got %q", got)
	}
	member = &discordgo.Member{User: &discordgo.User{GlobalName: "グローバル"}}
	if got := displayNameFromMember(member); got != "グローバル" {
		t.Errorf("fallback to user, got %q", got)
	}
	if got := displayNameFromMember(nil); got != "" {
		t.Errorf("nil member should return empty, got %q", got)
	}
}
