// Package discord は discordgo を使った Discord API へのアダプタを提供する。
package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// BotUserID は REST でボット自身のユーザー ID を取得する (gateway 接続は不要)。
func BotUserID(session *discordgo.Session) (string, error) {
	user, err := session.User("@me")
	if err != nil {
		return "", fmt.Errorf("failed to fetch bot user: %w", err)
	}
	return user.ID, nil
}
