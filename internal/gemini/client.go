// Package gemini は Gemini API を使った要約生成のアダプタを提供する。
package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"google.golang.org/genai"

	"yukan/internal/summary"
)

const defaultModel = "gemini-2.5-flash"

type highlightDecodeError struct {
	raw string
	err error
}

func (e *highlightDecodeError) Error() string {
	if e == nil || e.err == nil {
		return "failed to decode gemini highlights"
	}
	return fmt.Sprintf("failed to decode gemini highlights: %v", e.err)
}

func (e *highlightDecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *highlightDecodeError) RawResponse() string {
	if e == nil {
		return ""
	}
	return e.raw
}

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

// Summarize sends the prompt to Gemini and returns structured highlights.
func (c *Client) Summarize(ctx context.Context, prompt string) ([]summary.Highlight, error) {
	trimmedPrompt := strings.TrimSpace(prompt)
	if trimmedPrompt == "" {
		slog.WarnContext(ctx, "gemini prompt is empty")
		return nil, fmt.Errorf("prompt is empty")
	}
	if c == nil || c.apiKey == "" {
		slog.ErrorContext(ctx, "gemini client is not configured")
		return nil, fmt.Errorf("gemini client is not configured")
	}

	if forceEmptyHighlightsEnabled() {
		slog.WarnContext(ctx, "forcing empty highlights via YUKAN_FORCE_EMPTY_HIGHLIGHTS")
		return []summary.Highlight{}, nil
	}

	cfg := &genai.ClientConfig{APIKey: c.apiKey}

	gClient, err := genai.NewClient(ctx, cfg)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create gemini client", "error", err)
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

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
	resp, err := gClient.Models.GenerateContent(ctx, c.model, genai.Text(trimmedPrompt), config)
	if err != nil {
		slog.ErrorContext(ctx, "gemini generateContent failed", "model", c.model, "error", err)
		return nil, fmt.Errorf("failed to call gemini generateContent: %w", err)
	}

	raw, err := extractResponsePayload(resp)
	if err != nil {
		slog.ErrorContext(ctx, "failed to extract gemini payload", "error", err)
		return nil, fmt.Errorf("failed to read gemini response: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		slog.WarnContext(ctx, "gemini returned empty payload")
		return nil, fmt.Errorf("gemini returned empty summary")
	}

	highlights, err := summary.ParseHighlights(raw)
	if err != nil {
		slog.ErrorContext(ctx, "failed to parse gemini highlights", "error", err)
		return nil, &highlightDecodeError{raw: raw, err: err}
	}
	slog.InfoContext(ctx, "parsed gemini highlights", "count", len(highlights))

	return highlights, nil
}

func forceEmptyHighlightsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("YUKAN_FORCE_EMPTY_HIGHLIGHTS"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
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
