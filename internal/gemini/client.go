package gemini

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
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
func (c *Client) Summarize(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt is empty")
	}
	if c == nil || c.apiKey == "" {
		return "", fmt.Errorf("gemini client is not configured")
	}

	cfg := &genai.ClientConfig{APIKey: c.apiKey}

	gClient, err := genai.NewClient(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create gemini client: %w", err)
	}

	resp, err := gClient.Models.GenerateContent(ctx, c.model, genai.Text(prompt), nil)
	if err != nil {
		return "", fmt.Errorf("failed to call gemini generateContent: %w", err)
	}

	text, err := resp.Text()
	if err != nil {
		return "", fmt.Errorf("failed to read gemini response text: %w", err)
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("gemini returned empty summary")
	}

	return text, nil
}
