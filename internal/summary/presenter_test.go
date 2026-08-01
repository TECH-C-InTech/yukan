package summary

import (
	"strings"
	"testing"
)

func TestParseHighlights(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		raw := `[{"title":"見出し","emoji":"🎉","description":"本文","link":"https://discord.com/channels/1/2/3","color":"#ff0000"}]`
		got, err := ParseHighlights(raw)
		if err != nil {
			t.Fatalf("ParseHighlights error = %v", err)
		}
		if len(got) != 1 || got[0].Title != "見出し" || got[0].Color != "#ff0000" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("trims and filters incomplete entries", func(t *testing.T) {
		raw := `[
			{"title":" a ","emoji":"","description":" b ","link":" https://x ","color":""},
			{"title":"no link","emoji":"","description":"desc","link":"","color":""}
		]`
		got, err := ParseHighlights(raw)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].Title != "a" || got[0].Link != "https://x" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("errors", func(t *testing.T) {
		for name, raw := range map[string]string{
			"empty":        "",
			"broken json":  `[{`,
			"all filtered": `[{"title":"","description":"","link":""}]`,
		} {
			if _, err := ParseHighlights(raw); err == nil {
				t.Errorf("%s: should fail", name)
			}
		}
	})
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		input  string
		want   int
		wantOK bool
	}{
		{"#ff0000", 0xff0000, true},
		{"0xB0B0B0", 0xb0b0b0, true},
		{"00ff00", 0x00ff00, true},
		{" #123456 ", 0x123456, true},
		{"", 0, false},
		{"#fff", 0, false},
		{"zzzzzz", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseHexColor(tt.input)
		if ok != tt.wantOK || (ok && got != tt.want) {
			t.Errorf("parseHexColor(%q) = (%#x, %v), want (%#x, %v)", tt.input, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestComposeFinalMessage(t *testing.T) {
	highlights := []Highlight{{Title: "t", Description: "d", Link: "l"}}

	t.Run("with highlights returns dated header", func(t *testing.T) {
		got := composeFinalMessage(highlights, "fallback", 1800)
		if !strings.HasPrefix(got, "[**夕刊**] ") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("no highlights returns fallback", func(t *testing.T) {
		if got := composeFinalMessage(nil, " fb ", 1800); got != "fb" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("no highlights no fallback", func(t *testing.T) {
		if got := composeFinalMessage(nil, "", 1800); got != "" {
			t.Errorf("got %q", got)
		}
	})
}

func TestBuildSummaryMessage(t *testing.T) {
	digests := []ChannelDigest{{
		Name: "general",
		Messages: []MessageDigest{{
			Author:    "naoki",
			AvatarURL: "https://avatar",
			Link:      "https://discord.com/channels/1/2/3",
		}},
	}}

	t.Run("builds embeds and resolves author", func(t *testing.T) {
		highlights := []Highlight{{
			Title:       "見出し",
			Emoji:       "🎉",
			Description: "本文",
			Link:        "https://discord.com/channels/1/2/3",
			Color:       "#ff0000",
		}}
		header, embeds := BuildSummaryMessage("ヘッダー", highlights, digests)
		if header != "ヘッダー" {
			t.Errorf("header = %q", header)
		}
		if len(embeds) != 1 {
			t.Fatalf("embeds = %d, want 1", len(embeds))
		}
		e := embeds[0]
		if e.Title != "見出し 🎉" || e.Color != 0xff0000 || e.URL != "https://discord.com/channels/1/2/3" {
			t.Errorf("embed = %+v", e)
		}
		if e.Author == nil || e.Author.Name != "naoki" {
			t.Errorf("author should resolve from digests, got %+v", e.Author)
		}
	})

	t.Run("unknown link keeps embed without author", func(t *testing.T) {
		highlights := []Highlight{{Title: "t", Description: "d", Link: "https://discord.com/channels/9/9/9"}}
		_, embeds := BuildSummaryMessage("h", highlights, digests)
		if len(embeds) != 1 || embeds[0].Author != nil {
			t.Errorf("got %+v", embeds)
		}
		if embeds[0].Color != defaultHighlightColor {
			t.Errorf("color = %#x, want default", embeds[0].Color)
		}
	})

	t.Run("no highlights means not publishable", func(t *testing.T) {
		header, embeds := BuildSummaryMessage("h", nil, nil)
		if header != "h" || embeds != nil {
			t.Errorf("got (%q, %v)", header, embeds)
		}
	})

	t.Run("non-discord link is not linked", func(t *testing.T) {
		highlights := []Highlight{{Title: "t", Description: "d", Link: "https://evil.example.com/x"}}
		_, embeds := BuildSummaryMessage("h", highlights, digests)
		if len(embeds) != 1 {
			t.Fatalf("highlight should be kept, embeds = %d", len(embeds))
		}
		if embeds[0].URL != "" || embeds[0].Author != nil {
			t.Errorf("external URL should not be linked: %+v", embeds[0])
		}
	})

	t.Run("incomplete highlights are dropped", func(t *testing.T) {
		highlights := []Highlight{{Title: "", Description: "d", Link: "l"}}
		_, embeds := BuildSummaryMessage("h", highlights, nil)
		if embeds != nil {
			t.Errorf("got %v", embeds)
		}
	})
}
