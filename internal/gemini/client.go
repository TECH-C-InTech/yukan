package gemini

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"google.golang.org/genai"

	"yukan/internal/commands"
)

const defaultModel = "gemini-2.0-flash"

// Client wraps the official Gemini Go SDK to generate summaries.
type Client struct {
	apiKey string
	model  string
}

// New constructs a summarizer backed by Gemini. apiKey must be non-empty.
func New(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  defaultModel,
	}
}

// WithModel overrides the default model identifier.
func (c *Client) WithModel(model string) *Client {
	if model == "" {
		return c
	}
	clone := *c
	clone.model = model
	return &clone
}

// Summarize sends the prompt to Gemini and returns the condensed text.
func (c *Client) Summarize(ctx context.Context, digests []commands.ChannelDigest) (string, error) {
	if len(digests) == 0 {
		return "", fmt.Errorf("no digests provided")
	}
	if c == nil || c.apiKey == "" {
		return "", fmt.Errorf("gemini client is not configured")
	}

	cfg := &genai.ClientConfig{APIKey: c.apiKey}

	gClient, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create gemini client: %w", err)
	}

	// System prompt
	systemPrompt := buildSystemPrompt()
	if systemPrompt == nil {
		return "", fmt.Errorf("failed to build system prompt")
	}

	genConfig := &genai.GenerateContentConfig{
		SystemInstruction: systemPrompt,
	}

	contents := c.buildSummaryContents(ctx, digests)
	if len(contents) == 0 {
		return "", fmt.Errorf("no contents to summarize")
	}

	resp, err := gClient.Models.GenerateContent(ctx, c.model, contents, genConfig)
	if err != nil {
		return "", fmt.Errorf("failed to call gemini generateContent: %w", err)
	}

	text := resp.Text()
	if text == "" {
		return "", fmt.Errorf("failed to read gemini response text: %w", err)
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("gemini returned empty summary")
	}

	return text, nil
}

func buildSystemPrompt() *genai.Content {
	var builder strings.Builder
	builder.WriteString("あなたはDiscordサーバーの編集者です。以下は過去24時間に投稿されたメッセージ一覧です。最も注目すべき出来事を最大3件選び、指定のフォーマットで日本語の夕刊ハイライトを作成してください。\n")
	builder.WriteString("\n出力要件:\n")
	builder.WriteString("- Markdownを使用すること。\n")
	builder.WriteString("- 各トピックは `### 絵文字[キャッチーなタイトル](URL)（改行）コメント` の形式にすること。\n")
	builder.WriteString("- 例\n")
	builder.WriteString("  ### 🎉[明日のイベント参加希望者が続々！](https://discord.com/channels/...)\n")
	builder.WriteString("  会場は池袋に決定、参加希望者はリアクションを。\n")
	builder.WriteString("- トピック数は最大3件（重要度が低ければ1～2件でも可）。\n")
	builder.WriteString("- コメントは簡潔に1～2文でまとめること。\n")
	builder.WriteString("- `詳細` のリンクには提供されたメッセージURLを必ず1件使用すること。\n")
	builder.WriteString("- 指定の形式以外の文章やヘッダ・フッタは出力しないこと。\n")

	systemInstruction := &genai.Content{
		Parts: []*genai.Part{
			genai.NewPartFromText(builder.String()),
		},
	}

	return systemInstruction
}

func (c *Client) buildSummaryContents(ctx context.Context, digests []commands.ChannelDigest) (contents []*genai.Content) {
	if len(digests) == 0 {
		return nil
	}

	for _, digest := range digests {
		var builder strings.Builder
		builder.WriteString("\n# ")
		builder.WriteString(digest.Name)
		builder.WriteString("\n")
		for i := len(digest.Messages) - 1; i >= 0; i-- {
			msg := digest.Messages[i]
			builder.WriteString("- ")
			builder.WriteString(msg.Timestamp.Format("01/02 15:04"))
			builder.WriteString(" ")
			builder.WriteString(msg.Author)
			builder.WriteString(" ")
			builder.WriteString(digest.Name)
			if msg.Link != "" {
				builder.WriteString(" ")
				builder.WriteString(msg.Link)
			}
			builder.WriteString(": ")
			builder.WriteString(msg.Content)
			builder.WriteString("\n")

			parts := []*genai.Part{
				genai.NewPartFromText(builder.String()),
			}

			if len(msg.AttachmentInfo) > 0 {
				uploadedFiles, _, err := c.UploadAttachments(ctx, msg.AttachmentInfo)
				if err != nil {
					log.Printf("failed to upload attachments: %v", err)
					continue
				}
				for _, f := range uploadedFiles {
					if f == nil || f.Name == "" || f.URI == "" {
						continue
					}
					parts = append(parts, genai.NewPartFromURI(f.URI, f.MIMEType))
				}
			}

			contents = append(contents, genai.NewContentFromParts(parts, genai.RoleUser))
		}
	}

	return contents
}

// UploadURLConfig は UploadFromURL 用のオプションです。
type UploadURLConfig struct {
	// MIMEType は検出結果を上書きします。空の場合は HTTP ヘッダー、最後に URL 拡張子から推定します。
	MIMEType string
	// Name は任意のファイル名。指定し、かつ "files/" で始まらない場合は SDK 側で付与されます。
	Name string
	// DisplayName は人間に読みやすい表示名です。
	DisplayName string
	// ContentLength は既知のサイズ（バイト）。指定された場合はヘッダに反映して最適化します。
	ContentLength int64
}

func (c *Client) CheckAllFilesUploaded(ctx context.Context, files []*genai.File) (bool, error) {

	if c == nil || c.apiKey == "" {
		return false, fmt.Errorf("gemini client is not configured")
	}
	gClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: c.apiKey})
	if err != nil {
		return false, fmt.Errorf("failed to create gemini client: %w", err)
	}

	for {
		allActive := true
		for _, f := range files {
			if f == nil || f.Name == "" {
				continue
			}
			latest, err := gClient.Files.Get(ctx, f.Name, nil)
			if err != nil {
				return false, fmt.Errorf("failed to get file %s: %w", f.Name, err)
			}
			if latest == nil {
				return false, fmt.Errorf("file %s not found", f.Name)
			}
			if latest.State == genai.FileStateFailed {
				return false, fmt.Errorf("file %s upload failed", f.Name)
			}
			if latest.State == genai.FileStateProcessing {
				allActive = false
			}
		}
		if allActive {
			return true, nil
		}

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(2 * time.Second):
			// 再試行
		}
	}
}

// UploadFromURL は指定した URL からダウンロードし、そのストリームを Gemini Files にアップロードします。
// gClient.Files.UploadFromPathを参考に実装しています。
func (c *Client) UploadFromURL(ctx context.Context, rawURL string, cfg *UploadURLConfig) (*genai.File, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("gemini client is not configured")
	}
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("url is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build http request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to download url: status %d", resp.StatusCode)
	}

	// Gemini Files API limit: up to 2 GiB per file (safety check before upload)
	const maxFileSize int64 = 2 * 1024 * 1024 * 1024 // 2 GiB
	// Prefer explicit size from cfg if provided, otherwise use response Content-Length
	knownSize := resp.ContentLength
	if cfg != nil && cfg.ContentLength > 0 {
		knownSize = cfg.ContentLength
	}
	if knownSize > maxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes exceeds 2GiB limit", knownSize)
	}

	// MIME 判定: 明示指定 > ヘッダー > 拡張子
	mimeType := ""
	if cfg != nil && cfg.MIMEType != "" {
		mimeType = cfg.MIMEType
	}
	if mimeType == "" {
		mimeType = resp.Header.Get("Content-Type")
		if idx := strings.Index(mimeType, ";"); idx > 0 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}
	}
	if mimeType == "" {
		ext := path.Ext(req.URL.Path)
		if ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
	}
	if mimeType == "" {
		return nil, fmt.Errorf("could not determine MIME type; set UploadURLConfig.MIMEType")
	}

	upCfg := &genai.UploadFileConfig{MIMEType: mimeType}
	if cfg != nil {
		upCfg.Name = cfg.Name
		upCfg.DisplayName = cfg.DisplayName
	}
	if upCfg.DisplayName == "" {
		// URL パスから見かけのファイル名を補完
		base := path.Base(req.URL.Path)
		if base != "/" && base != "." && base != "" {
			upCfg.DisplayName = base
		}
	}
	// Content-Length が分かる場合はヘッダーを伝搬（最適化）。
	contentLen := knownSize
	if contentLen > 0 {
		upCfg.HTTPOptions = &genai.HTTPOptions{Headers: http.Header{}}
		upCfg.HTTPOptions.Headers.Add("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", contentLen))
	}

	gClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: c.apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}
	// io.Reader をそのまま渡してストリームアップロード。
	return gClient.Files.Upload(ctx, io.Reader(resp.Body), upCfg)
}

// TotalFilesUsage は現在の Files ストレージ使用量（バイト）を返します。
func (c *Client) TotalFilesUsage(ctx context.Context) (int64, error) {
	if c == nil || c.apiKey == "" {
		return 0, fmt.Errorf("gemini client is not configured")
	}
	gClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: c.apiKey})
	if err != nil {
		return 0, fmt.Errorf("failed to create gemini client: %w", err)
	}
	var total int64
	for file, err := range gClient.Files.All(ctx) {
		if err != nil {
			return 0, err
		}
		if file == nil {
			continue
		}
		if file.SizeBytes != nil {
			total += *file.SizeBytes
		}
	}
	return total, nil
}

// HasSpaceFor は、合計容量上限(capBytes)に対して、toUpload バイトの追加が可能かを返します。
// capBytes が 0 の場合は 20GiB を既定値として扱います。
func (c *Client) HasSpaceFor(ctx context.Context, toUpload int64, capBytes int64) (ok bool, remaining int64, err error) {
	if capBytes <= 0 {
		capBytes = 20 * 1024 * 1024 * 1024 // 20 GiB
	}
	used, err := c.TotalFilesUsage(ctx)
	if err != nil {
		return false, 0, err
	}
	if toUpload < 0 {
		toUpload = 0
	}
	if used+toUpload <= capBytes {
		return true, capBytes - (used + toUpload), nil
	}
	return false, capBytes - used, nil
}

// DeleteAllFiles deletes all files stored in Gemini Files for this API key.
// It attempts best-effort deletion of every file and returns the number of
// successfully deleted items along with the first error encountered (if any).
func (c *Client) DeleteAllFiles(ctx context.Context) (int, error) {
	if c == nil || c.apiKey == "" {
		return 0, fmt.Errorf("gemini client is not configured")
	}
	gClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: c.apiKey})
	if err != nil {
		return 0, fmt.Errorf("failed to create gemini client: %w", err)
	}
	deleted := 0
	var firstErr error
	for f, itErr := range gClient.Files.All(ctx) {
		if itErr != nil {
			if firstErr == nil {
				firstErr = itErr
			}
			continue
		}
		if f == nil || f.Name == "" {
			continue
		}
		if _, delErr := gClient.Files.Delete(ctx, f.Name, nil); delErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("delete %s: %w", f.Name, delErr)
			}
			continue
		}
		deleted++
	}
	return deleted, firstErr
}

// UploadAttachments uploads message attachments in batch to Gemini Files.
// It performs per-file 2GiB checks (via attachment.Size) and an optional total-capacity
// check against 20GiB (current total usage + sum of selected attachments).
// It returns the uploaded File resources' names and a list of skipped reasons per attachment index.
func (c *Client) UploadAttachments(ctx context.Context, atts []commands.AttachmentInfo) (uploadedFiles []*genai.File, skipped map[int]string, err error) {
	uploadedFiles = []*genai.File{}
	skipped = map[int]string{}
	if c == nil || len(atts) == 0 {
		return uploadedFiles, skipped, nil
	}

	// Pre-filter: enforce 2GiB/file
	const twoGiB int64 = 2 * 1024 * 1024 * 1024
	var totalToUpload int64
	for i, att := range atts {
		if att.Size > 0 && int64(att.Size) > twoGiB {
			skipped[i] = "exceeds 2GiB/file limit"
			continue
		}
		totalToUpload += int64(att.Size)
	}

	// Capacity check against 20GiB total (best-effort; relies on stored files meta)
	if totalToUpload > 0 {
		ok, _, capErr := c.HasSpaceFor(ctx, totalToUpload, 0)
		if capErr != nil {
			return nil, skipped, fmt.Errorf("failed capacity check: %w", capErr)
		}
		if !ok {
			return nil, skipped, fmt.Errorf("insufficient total capacity for %d bytes", totalToUpload)
		}
	}

	// Upload sequentially (could be parallelized carefully with context limits)
	for i, att := range atts {
		if _, wasSkipped := skipped[i]; wasSkipped {
			continue
		}
		url := att.ProxyURL
		if url == "" {
			url = att.URL
		}
		cfg := &UploadURLConfig{
			MIMEType:      att.ContentType,
			DisplayName:   att.Filename,
			ContentLength: int64(att.Size),
		}
		fileRes, upErr := c.UploadFromURL(ctx, url, cfg)
		if upErr != nil {
			skipped[i] = upErr.Error()
			continue
		}
		if fileRes != nil {
			uploadedFiles = append(uploadedFiles, fileRes)
		}
	}

	// Wait until all uploaded files are active
	_, waitErr := c.CheckAllFilesUploaded(ctx, uploadedFiles)
	if waitErr != nil {
		return uploadedFiles, skipped, fmt.Errorf("some files failed to upload: %w", waitErr)
	}

	return uploadedFiles, skipped, nil
}
