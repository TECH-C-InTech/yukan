package commands

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	getCommandName        = "get"
	getCommandDescription = "Fetch all messages from the last 24 hours in each accessible text channel."
	messageCharBudget     = 1800
	fetchLimit            = 100
	lookbackWindow        = 24 * time.Hour
)

var allowedGuildIDs = map[string]struct{}{
	"1417771222355152980": {},
	"1239827951667908668": {},
	"1216568461661048902": {},
}

// GetCommand implements the /get slash command.
type GetCommand struct {
	definition *discordgo.ApplicationCommand
	summarizer Summarizer
}

// NewGetCommand constructs a GetCommand instance ready to be registered.
func NewGetCommand(s Summarizer) *GetCommand {
	return &GetCommand{
		definition: &discordgo.ApplicationCommand{
			Name:        getCommandName,
			Description: getCommandDescription,
		},
		summarizer: s,
	}
}

// Name returns the slash command name.
func (c *GetCommand) Name() string {
	return c.definition.Name
}

// Definition returns the ApplicationCommand payload.
func (c *GetCommand) Definition() *discordgo.ApplicationCommand {
	return c.definition
}

// Handle processes the interaction for the /get command.
func (c *GetCommand) Handle(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
	if interaction.Type != discordgo.InteractionApplicationCommand {
		return
	}

	workflowStart := time.Now()

	if interaction.GuildID == "" {
		c.respond(session, interaction, "このコマンドはサーバー内のみで利用できます")
		return
	}

	if _, ok := allowedGuildIDs[interaction.GuildID]; !ok {
		c.respond(session, interaction, "このサーバーでは /get コマンドを利用できません")
		return
	}

	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{},
	}); err != nil {
		log.Printf("failed to send initial response: %v", err)
		return
	}

	summaryCtx := WithSummaryStart(context.Background(), workflowStart)
	finalMessage, highlights, digests, err := GenerateSummaryForGuild(summaryCtx, session, c.summarizer, interaction.GuildID)
	if err != nil {
		msg := fmt.Sprintf("エラーが発生しました: %v", err)
		log.Printf("/%s command failed: %v", c.Name(), err)
		if _, editErr := session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &msg}); editErr != nil {
			log.Printf("failed to send error message: %v", editErr)
		}
		return
	}

	content, embeds := BuildSummaryMessage(finalMessage, highlights, digests)
	if len(embeds) > 0 {
		if content == "" {
			content = finalMessage
		}
		contentCopy := content
		embedCopy := embeds
		_, err = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &contentCopy, Embeds: &embedCopy})
	} else {
		finalCopy := finalMessage
		_, err = session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &finalCopy})
	}
	if err != nil {
		log.Printf("failed to send response: %v", err)
		return
	}

	NotifySummaryLog(session, fmt.Sprintf("出力送信完了 (/get, channel=%s, 累計%.2fs)", interaction.ChannelID, time.Since(workflowStart).Seconds()))
}

func (c *GetCommand) respond(session *discordgo.Session, interaction *discordgo.InteractionCreate, message string) {
	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: message},
	}); err != nil {
		log.Printf("failed to send restricted response: %v", err)
	}
}
