package summary

import (
	"unicode/utf8"
)

func truncateRunes(input string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(input) <= limit {
		return input
	}
	return string([]rune(input)[:limit]) + "…"
}
