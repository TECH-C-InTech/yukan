// Package gemini は Gemini API を使った要約生成のアダプタを提供する。
package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	defaultModel = "gemini-2.5-flash"
	// 1回の生成呼び出しの上限。ハングした接続でリトライ機会を失わないための保険
	requestTimeout = 2 * time.Minute
)

// Client wraps the official Gemini Go SDK.
type Client struct {
	client *genai.Client
	model  string
}

// New は Gemini クライアントを生成する。内部の genai クライアントは
// ここで1度だけ作り、呼び出しごとに使い回す。
func New(ctx context.Context, apiKey string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("gemini api key is empty")
	}
	gClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}
	return &Client{client: gClient, model: defaultModel}, nil
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

// Summarize sends the prompt to Gemini and returns the raw JSON payload.
// パースはドメイン側 (summary) の責務とし、このアダプタは summary に依存しない。
func (c *Client) Summarize(ctx context.Context, prompt string) (string, error) {
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		return "", fmt.Errorf("prompt is empty")
	}
	if c == nil || c.client == nil {
		return "", fmt.Errorf("gemini client is not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"title":       {Type: genai.TypeString},
					"emoji":       {Type: genai.TypeString},
					"description": {Type: genai.TypeString},
					"link":        {Type: genai.TypeString},
					"color":       {Type: genai.TypeString},
				},
				PropertyOrdering: []string{"title", "emoji", "description", "link", "color"},
				Required:         []string{"title", "emoji", "description", "link", "color"},
			},
		},
	}

	slog.InfoContext(ctx, "invoking gemini", "model", c.model, "prompt_length", len(trimmedPrompt))
	resp, err := c.client.Models.GenerateContent(ctx, c.model, genai.Text(trimmedPrompt), config)
	if err != nil {
		slog.ErrorContext(ctx, "gemini generateContent failed", "model", c.model, "error", err)
		return "", fmt.Errorf("failed to call gemini generateContent: %w", err)
	}

	raw, err := extractResponsePayload(resp)
	if err != nil {
		slog.ErrorContext(ctx, "failed to extract gemini payload", "error", err)
		return "", fmt.Errorf("failed to read gemini response: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("gemini returned empty summary")
	}
	return raw, nil
}

func extractResponsePayload(resp *genai.GenerateContentResponse) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("gemini response is nil")
	}

	if trimmed := strings.TrimSpace(resp.Text()); trimmed != "" {
		return trimmed, nil
	}

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("gemini returned no candidates")
	}

	candidate := resp.Candidates[0]
	if candidate == nil || candidate.Content == nil {
		return "", fmt.Errorf("gemini candidate contained no content")
	}

	for _, part := range candidate.Content.Parts {
		if part == nil || part.Thought {
			continue
		}
		if part.InlineData != nil && len(part.InlineData.Data) > 0 {
			payload := strings.TrimSpace(string(part.InlineData.Data))
			if payload != "" {
				return payload, nil
			}
		}
		if part.Text != "" {
			if trimmed := strings.TrimSpace(part.Text); trimmed != "" {
				return trimmed, nil
			}
		}
	}

	return "", fmt.Errorf("gemini response contained no usable content")
}
