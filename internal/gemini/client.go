package gemini

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"yukan/internal/commands"
)

const defaultModel = "gemini-2.5-flash"

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
func (c *Client) Summarize(ctx context.Context, prompt string) ([]commands.Highlight, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt is empty")
	}
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("gemini client is not configured")
	}

	cfg := &genai.ClientConfig{APIKey: c.apiKey}

	gClient, err := genai.NewClient(ctx, cfg)
	if err != nil {
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
				Required:         []string{"title", "description", "link"},
			},
		},
	}

	resp, err := gClient.Models.GenerateContent(ctx, c.model, genai.Text(prompt), config)
	if err != nil {
		return nil, fmt.Errorf("failed to call gemini generateContent: %w", err)
	}

	raw, err := extractResponsePayload(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read gemini response: %w", err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("gemini returned empty summary")
	}

	highlights, err := commands.ParseHighlights(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode gemini highlights: %w", err)
	}

	return highlights, nil
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
