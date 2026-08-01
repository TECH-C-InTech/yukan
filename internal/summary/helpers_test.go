package summary

import (
	"testing"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit int
		want  string
	}{
		{"within limit", "hello", 10, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello…"},
		{"zero limit", "hello", 0, ""},
		{"negative limit", "hello", -1, ""},
		{"multibyte within", "こんにちは", 5, "こんにちは"},
		{"multibyte over", "こんにちは世界", 5, "こんにちは…"},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateRunes(tt.input, tt.limit); got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.want)
			}
		})
	}
}
