package gemini

import (
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestExtractResponsePayload(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		if _, err := extractResponsePayload(nil); err == nil {
			t.Error("should fail on nil response")
		}
	})

	t.Run("no candidates", func(t *testing.T) {
		if _, err := extractResponsePayload(&genai.GenerateContentResponse{}); err == nil {
			t.Error("should fail without candidates")
		}
	})

	t.Run("text part", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{
				Content: &genai.Content{Parts: []*genai.Part{{Text: "  payload  "}}},
			}},
		}
		got, err := extractResponsePayload(resp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got != "payload" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("thought parts are skipped", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{
				Content: &genai.Content{Parts: []*genai.Part{
					{Text: "thinking...", Thought: true},
					{Text: "answer"},
				}},
			}},
		}
		got, err := extractResponsePayload(resp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got != "answer" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("inline data", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{
				Content: &genai.Content{Parts: []*genai.Part{
					{InlineData: &genai.Blob{Data: []byte(" [1,2] ")}},
				}},
			}},
		}
		got, err := extractResponsePayload(resp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got != "[1,2]" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{
				Content: &genai.Content{Parts: []*genai.Part{{Text: "   "}}},
			}},
		}
		if _, err := extractResponsePayload(resp); err == nil {
			t.Error("should fail on whitespace-only content")
		}
	})
}

func TestWithModel(t *testing.T) {
	c, err := New(t.Context(), "key")
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if c.model != defaultModel {
		t.Errorf("default model = %q", c.model)
	}
	c2 := c.WithModel("gemini-2.5-pro")
	if c2.model != "gemini-2.5-pro" || c.model != defaultModel {
		t.Errorf("WithModel should clone: c=%q c2=%q", c.model, c2.model)
	}
	if c3 := c.WithModel(""); c3.model != defaultModel {
		t.Errorf("empty model should keep default")
	}
}

func TestSummarizeValidation(t *testing.T) {
	c, err := New(t.Context(), "key")
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if _, err := c.Summarize(t.Context(), "   "); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty prompt should fail, got %v", err)
	}
	unconfigured := &Client{}
	if _, err := unconfigured.Summarize(t.Context(), "prompt"); err == nil {
		t.Error("unconfigured client should fail")
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(t.Context(), "  "); err == nil {
		t.Error("New should fail with empty api key")
	}
}
