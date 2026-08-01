package summary

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Collector fetches recent messages from Discord.
type Collector struct {
	Session    *discordgo.Session
	Lookback   time.Duration
	FetchLimit int
	// MaxConcurrency controls how many channels are fetched in parallel.
	MaxConcurrency int
}

const (
	fallbackNoMessages = "過去24時間のメッセージはありません"
	fallbackGeneric    = "夕刊を生成できませんでした。しばらくしてから再実行してください。"
)

// Collect gathers messages from the target guild within the configured lookback window.
func (c *Collector) Collect(ctx context.Context, guildID, botID string) (CollectorResult, error) {
	var empty CollectorResult

	if guildID == "" {
		return empty, fmt.Errorf("guild id is required")
	}
	if c == nil || c.Session == nil {
		return empty, fmt.Errorf("discord session is not configured")
	}

	baseChannels, err := c.Session.GuildChannels(guildID)
	if err != nil {
		return empty, fmt.Errorf("チャンネル一覧の取得に失敗しました: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(-c.Lookback)

	var (
		channels    []*discordgo.Channel
		seen        = make(map[string]struct{})
		parentNames = make(map[string]string)
	)

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

	if threads, err := c.Session.GuildThreadsActive(guildID); err == nil && threads != nil {
		for _, th := range threads.Threads {
			appendChannel(th)
		}
	} else if err != nil {
		log.Printf("collector: failed to fetch active threads: %v", err)
	}

	for _, ch := range baseChannels {
		if !isThreadParentChannel(ch.Type) {
			continue
		}
		threads, err := c.Session.ThreadsArchived(ch.ID, nil, 100)
		if err != nil {
			log.Printf("collector: failed to fetch archived threads for channel %s: %v", ch.ID, err)
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

	memberNames := newNameCache()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]*ChannelDigest, len(channels))
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	sem := make(chan struct{}, c.MaxConcurrency)

	for idx, ch := range channels {
		if ch == nil || !isTextChannel(ch.Type) {
			continue
		}

		wg.Add(1)
		go func(index int, channel *discordgo.Channel) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			digest, err := c.collectChannel(ctx, guildID, botID, cutoff, channel, parentNames, memberNames)
			if err != nil {
				setErr(err)
				cancel()
				return
			}
			if digest == nil {
				return
			}
			results[index] = digest
		}(idx, ch)
	}

	wg.Wait()

	if firstErr != nil {
		return empty, firstErr
	}

	var (
		digests            []ChannelDigest
		totalCollectedMsgs int
	)

	for _, digest := range results {
		if digest == nil {
			continue
		}
		digests = append(digests, *digest)
		totalCollectedMsgs += len(digest.Messages)
	}

	fallback := fallbackMessage(totalCollectedMsgs)

	return CollectorResult{
		Digests:       digests,
		FallbackText:  fallback,
		TotalMessages: totalCollectedMsgs,
	}, nil
}

func fallbackMessage(total int) string {
	if total <= 0 {
		return fallbackNoMessages
	}
	return fallbackGeneric
}

func (c *Collector) collectChannel(ctx context.Context, guildID, botID string, cutoff time.Time, ch *discordgo.Channel, parentNames map[string]string, memberNames *nameCache) (*ChannelDigest, error) {
	displayName := channelDisplayName(ch, parentNames)

	collected := make([]MessageDigest, 0, c.FetchLimit)
	beforeID := ""
	failedFetch := false

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		messages, err := c.Session.ChannelMessages(ch.ID, c.FetchLimit, beforeID, "", "")
		if err != nil {
			log.Printf("collector: failed to fetch messages for channel %s: %v", ch.ID, err)
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

			digest := MessageDigest{
				Author:         resolveAuthor(c.Session, guildID, msg, memberNames),
				AvatarURL:      userAvatarURL(msg.Author),
				Content:        extractContent(msg),
				Timestamp:      timestamp,
				MessageID:      msg.ID,
				ChannelID:      ch.ID,
				Link:           buildMessageLink(guildID, ch.ID, msg.ID),
				AttachmentURLs: extractAttachmentURLs(msg),
			}

			collected = append(collected, digest)
		}

		if reachedOlderThanWindow || len(messages) < c.FetchLimit {
			break
		}

		beforeID = messages[len(messages)-1].ID
	}

	if failedFetch || len(collected) == 0 {
		return nil, nil
	}

	return &ChannelDigest{Name: displayName, Messages: collected}, nil
}

func channelDisplayName(ch *discordgo.Channel, parentNames map[string]string) string {
	if ch == nil {
		return ""
	}
	if isThreadChannel(ch.Type) {
		if parent, ok := parentNames[ch.ParentID]; ok && parent != "" {
			return fmt.Sprintf("%s › %s", parent, ch.Name)
		}
	}
	return ch.Name
}

func resolveAuthor(session *discordgo.Session, guildID string, msg *discordgo.Message, cache *nameCache) string {
	const unknown = "Unknown"
	if msg == nil {
		return unknown
	}

	putAndReturn := func(userID, name string) string {
		if cache != nil && userID != "" && name != "" {
			cache.Set(userID, name)
		}
		return name
	}

	if name := displayNameFromMember(msg.Member); name != "" {
		uid := ""
		if msg.Author != nil {
			uid = msg.Author.ID
		}
		return putAndReturn(uid, name)
	}

	userID := ""
	if msg.Author != nil {
		userID = msg.Author.ID
	}

	if cache != nil && userID != "" {
		if cached, ok := cache.Get(userID); ok && cached != "" {
			return cached
		}
	}

	if session != nil && session.State != nil && guildID != "" && userID != "" {
		if member, err := session.State.Member(guildID, userID); err == nil {
			if name := displayNameFromMember(member); name != "" {
				return putAndReturn(userID, name)
			}
		}
	}

	if session != nil && guildID != "" && userID != "" {
		if member, err := session.GuildMember(guildID, userID); err == nil {
			if name := displayNameFromMember(member); name != "" {
				return putAndReturn(userID, name)
			}
		}
	}

	if msg.Author != nil {
		if name := displayNameFromUser(msg.Author); name != "" {
			return putAndReturn(userID, name)
		}
	}

	if cache != nil && userID != "" {
		cache.Set(userID, unknown)
	}
	return unknown
}

type nameCache struct {
	mu   sync.RWMutex
	data map[string]string
}

func newNameCache() *nameCache {
	return &nameCache{data: make(map[string]string)}
}

func (c *nameCache) Get(key string) (string, bool) {
	if c == nil || key == "" {
		return "", false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.data[key]
	return value, ok
}

func (c *nameCache) Set(key, value string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	c.data[key] = value
	c.mu.Unlock()
}

func extractContent(msg *discordgo.Message) string {
	if msg == nil {
		return "(内容なし)"
	}

	content := msg.Content
	if content == "" && len(msg.Attachments) > 0 {
		attachmentNames := make([]string, len(msg.Attachments))
		for i, att := range msg.Attachments {
			attachmentNames[i] = att.Filename
		}
		content = fmt.Sprintf("(添付ファイルあり: %s)", strings.Join(attachmentNames, ", "))
	}
	if content == "" {
		content = "(内容なし)"
	}

	return sanitizeContent(content)
}
