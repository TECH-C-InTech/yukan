package commands

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

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

// Summarizer generates a digest from the collected messages.
type Summarizer interface {
	Summarize(ctx context.Context, digests []ChannelDigest) (string, error)
}

// GetCommand implements the /get slash command.
type GetCommand struct {
	definition *discordgo.ApplicationCommand
	summarizer Summarizer
}

type AttachmentInfo struct {
	Filename    string
	ContentType string
	Size        int
	URL         string
	ProxyURL    string
}

type messageDigest struct {
	Author         string
	Content        string
	Timestamp      time.Time
	MessageID      string
	ChannelID      string
	Link           string
	AttachmentInfo []AttachmentInfo
}

type ChannelDigest struct {
	Name     string
	Messages []messageDigest
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

	finalMessage, err := GenerateSummaryForGuild(context.Background(), session, c.summarizer, interaction.GuildID)
	if err != nil {
		msg := fmt.Sprintf("エラーが発生しました: %v", err)
		log.Printf("/%s command failed: %v", c.Name(), err)
		if _, editErr := session.InteractionResponseEdit(interaction.Interaction, &discordgo.WebhookEdit{Content: &msg}); editErr != nil {
			log.Printf("failed to send error message: %v", editErr)
		}
		return
	}

	content, embeds := BuildSummaryMessage(finalMessage)
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
	}
}

func (c *GetCommand) respond(session *discordgo.Session, interaction *discordgo.InteractionCreate, message string) {
	if err := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: message},
	}); err != nil {
		log.Printf("failed to send restricted response: %v", err)
	}
}

func collectLatestMessages(session *discordgo.Session, guildID string, botID string) ([]ChannelDigest, string, error) {
	if guildID == "" {
		return nil, "このコマンドはサーバー内のみで利用できます", nil
	}

	baseChannels, err := session.GuildChannels(guildID)
	if err != nil {
		return nil, "", fmt.Errorf("チャンネル一覧の取得に失敗しました: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(-lookbackWindow)

	// Build combined list including threads.
	var channels []*discordgo.Channel
	seen := make(map[string]struct{})
	parentNames := make(map[string]string)

	appendChannel := func(ch *discordgo.Channel) {
		if ch == nil {
			return
		}
		if _, exists := seen[ch.ID]; exists {
			return
		}
		seen[ch.ID] = struct{}{}
		channels = append(channels, ch)
	}

	for _, ch := range baseChannels {
		parentNames[ch.ID] = ch.Name
		appendChannel(ch)
	}

	if threads, err := session.GuildThreadsActive(guildID); err == nil && threads != nil {
		for _, th := range threads.Threads {
			appendChannel(th)
		}
	} else if err != nil {
		log.Printf("failed to fetch active threads: %v", err)
	}

	for _, ch := range baseChannels {
		if !isThreadParentChannel(ch.Type) {
			continue
		}
		threads, err := session.ThreadsArchived(ch.ID, nil, 100)
		if err != nil {
			log.Printf("failed to fetch archived threads for channel %s: %v", ch.ID, err)
			continue
		}
		if threads == nil {
			continue
		}
		for _, th := range threads.Threads {
			if th == nil || th.ThreadMetadata == nil {
				continue
			}
			if th.ThreadMetadata.ArchiveTimestamp.Before(cutoff) {
				continue
			}
			appendChannel(th)
		}
	}

	var (
		digests        []ChannelDigest
		fallbackBlocks []string
	)

	for _, ch := range channels {
		if !isTextChannel(ch.Type) {
			continue
		}

		displayName := ch.Name
		if isThreadChannel(ch.Type) {
			if parent, ok := parentNames[ch.ParentID]; ok && parent != "" {
				displayName = fmt.Sprintf("%s › %s", parent, ch.Name)
			}
		}

		collected := make([]messageDigest, 0, fetchLimit)
		beforeID := ""
		failedFetch := false

		for {
			messages, err := session.ChannelMessages(ch.ID, fetchLimit, beforeID, "", "")
			if err != nil {
				log.Printf("failed to fetch messages for channel %s: %v", ch.ID, err)
				failedFetch = true
				break
			}
			if len(messages) == 0 {
				break
			}

			reachedOlderThanWindow := false
			for _, msg := range messages {
				timestamp := msg.Timestamp
				if timestamp.Before(cutoff) {
					reachedOlderThanWindow = true
					break
				}

				if botID != "" && msg.Author != nil && msg.Author.ID == botID {
					continue
				}

				digest := messageDigest{
					Author:         resolveAuthor(msg),
					Content:        extractContent(msg),
					Timestamp:      timestamp,
					MessageID:      msg.ID,
					ChannelID:      ch.ID,
					Link:           buildMessageLink(guildID, ch.ID, msg.ID),
					AttachmentInfo: extractAttachmentInfo(msg),
				}

				collected = append(collected, digest)
			}

			if reachedOlderThanWindow || len(messages) < fetchLimit {
				break
			}

			beforeID = messages[len(messages)-1].ID
		}

		display := displayName
		switch {
		case failedFetch:
			fallbackBlocks = append(fallbackBlocks, fmt.Sprintf("#%s: メッセージを取得できませんでした", display))
		case len(collected) == 0:
			fallbackBlocks = append(fallbackBlocks, fmt.Sprintf("#%s: 過去24時間のメッセージはありません", display))
		default:
			digest := ChannelDigest{Name: display, Messages: collected}
			digests = append(digests, digest)
			fallbackBlocks = append(fallbackBlocks, renderChannelBlock(digest))
		}
	}

	fallback := strings.Join(fallbackBlocks, "\n\n")
	if strings.TrimSpace(fallback) == "" {
		fallback = "対象のテキストチャンネルが見つかりませんでした"
	}

	return digests, fallback, nil
}

// GenerateSummaryForGuild collects the latest messages and produces a final digest message.
func GenerateSummaryForGuild(ctx context.Context, session *discordgo.Session, summarizer Summarizer, guildID string) (string, error) {
	if session == nil {
		return "", fmt.Errorf("session is nil")
	}

	botID := ""
	if session.State != nil && session.State.User != nil {
		botID = session.State.User.ID
	}

	digests, fallback, err := collectLatestMessages(session, guildID, botID)
	if err != nil {
		return "", err
	}

	finalMessage := fallback
	if len(digests) > 0 && summarizer != nil {
		summaryCtx := ctx
		if summaryCtx == nil {
			summaryCtx = context.Background()
		}
		if _, hasDeadline := summaryCtx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			summaryCtx, cancel = context.WithTimeout(summaryCtx, 45*time.Second)
			defer cancel()
		}

		summary, err := summarizer.Summarize(summaryCtx, digests)
		if err != nil {
			log.Printf("failed to summarize messages: %v", err)
		} else if trimmed := strings.TrimSpace(summary); trimmed != "" {
			finalMessage = composeFinalMessage(trimmed, fallback)
		}
	}

	if strings.TrimSpace(finalMessage) == "" {
		finalMessage = fallback
	}

	if utf8.RuneCountInString(finalMessage) > messageCharBudget {
		finalMessage = truncateRunes(finalMessage, messageCharBudget)
	}

	return finalMessage, nil
}

func renderChannelBlock(digest ChannelDigest) string {
	lines := make([]string, 0, len(digest.Messages)+1)
	lines = append(lines, fmt.Sprintf("#%s", digest.Name))

	for i := len(digest.Messages) - 1; i >= 0; i-- {
		msg := digest.Messages[i]
		lines = append(lines, formatMessageLine(msg))
	}

	return strings.Join(lines, "\n")
}

func formatMessageLine(msg messageDigest) string {
	timestamp := msg.Timestamp.Format("01/02 15:04")
	link := msg.Link
	if link != "" {
		return fmt.Sprintf("- [%s] %s: %s (%s)", timestamp, msg.Author, truncateRunes(msg.Content, messageLineCharLimit), link)
	}
	return fmt.Sprintf("- [%s] %s: %s", timestamp, msg.Author, truncateRunes(msg.Content, messageLineCharLimit))
}

func composeFinalMessage(summary, fallback string) string {
	summary = strings.TrimSpace(summary)
	fallback = strings.TrimSpace(fallback)

	if summary == "" {
		return truncateRunes(fallback, messageCharBudget)
	}

	date := time.Now().Format("2006年1月2日")
	header := fmt.Sprintf("[夕刊] %s\n%s", date, summary)
	return truncateRunes(header, messageCharBudget)
}

// BuildSummaryMessage splits the final summary string into a header and embeds suitable for Discord.
// If parsing fails, it returns the original summary and nil embeds.
func BuildSummaryMessage(summary string) (string, []*discordgo.MessageEmbed) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", nil
	}

	lines := strings.Split(summary, "\n")
	if len(lines) == 0 {
		return summary, nil
	}

	header := strings.TrimSpace(lines[0])
	var embeds []*discordgo.MessageEmbed
	var current *discordgo.MessageEmbed
	var accumulator []string

	flush := func() {
		if current == nil {
			return
		}
		current.Description = strings.Join(accumulator, "\n")
		embeds = append(embeds, current)
		accumulator = accumulator[:0]
		current = nil
	}

	for _, raw := range lines[1:] {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "###") {
			flush()
			if embed := parseHighlightLine(line); embed != nil {
				current = embed
			} else {
				return summary, nil
			}
			continue
		}
		if current != nil {
			accumulator = append(accumulator, line)
		}
	}
	flush()

	if len(embeds) == 0 {
		return summary, nil
	}

	return header, embeds
}

func parseHighlightLine(line string) *discordgo.MessageEmbed {
	content := strings.TrimSpace(strings.TrimPrefix(line, "###"))
	if content == "" {
		return nil
	}

	idxBracket := strings.Index(content, "[")
	idxCloseBracket := strings.Index(content, "]")
	idxOpenParen := strings.Index(content, "(")
	idxCloseParen := strings.LastIndex(content, ")")

	if idxBracket == -1 || idxCloseBracket == -1 || idxOpenParen == -1 || idxCloseParen == -1 {
		return nil
	}
	if !(idxBracket < idxCloseBracket && idxCloseBracket < idxOpenParen && idxOpenParen < idxCloseParen) {
		return nil
	}

	emoji := strings.TrimSpace(content[:idxBracket])
	title := strings.TrimSpace(content[idxBracket+1 : idxCloseBracket])
	url := strings.TrimSpace(content[idxOpenParen+1 : idxCloseParen])

	titleText := strings.TrimSpace(strings.TrimSpace(emoji + " " + title))
	if titleText == "" {
		titleText = title
	}

	return &discordgo.MessageEmbed{
		Title: titleText,
		URL:   url,
	}
}
