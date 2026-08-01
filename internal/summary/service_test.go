package summary

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCollector struct {
	result CollectorResult
	err    error
	calls  int
}

func (f *fakeCollector) Collect(_ context.Context, _, _ string) (CollectorResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeSummarizer struct {
	responses []func() (string, error)
	calls     int
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ string) (string, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	return f.responses[idx]()
}

type fakeNotifier struct {
	infos  []string
	errors []string
}

func (f *fakeNotifier) Info(_ context.Context, msg string)  { f.infos = append(f.infos, msg) }
func (f *fakeNotifier) Error(_ context.Context, msg string) { f.errors = append(f.errors, msg) }

var testDigests = []ChannelDigest{{
	Name: "general",
	Messages: []MessageDigest{{
		Author:  "naoki",
		Content: "hello",
		Link:    "https://discord.com/channels/1/2/3",
	}},
}}

func newTestService(collector *fakeCollector, summarizer *fakeSummarizer, note *fakeNotifier) *Service {
	return &Service{
		Collector:  collector,
		Summarizer: summarizer,
		Notifier:   note,
		Config:     Config{MessageCharBudget: 1800, MaxHighlights: 3, MaxAttempts: 3},
		Oddity:     func() bool { return false },
		Backoff:    func(int) time.Duration { return 0 },
	}
}

func okHighlights() (string, error) {
	return `[{"title":"t","emoji":"","description":"d","link":"https://discord.com/channels/1/2/3","color":""}]`, nil
}

func TestGenerateSuccess(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){okHighlights}}
	note := &fakeNotifier{}

	result, err := newTestService(collector, summarizer, note).Generate(t.Context(), Request{TargetName: "dev", GuildID: "g"})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if !result.Publishable {
		t.Error("result should be publishable")
	}
	if len(result.Embeds) != 1 {
		t.Errorf("embeds = %d, want 1", len(result.Embeds))
	}
	if !strings.HasPrefix(result.Content, "[**夕刊**]") {
		t.Errorf("content = %q", result.Content)
	}
	if summarizer.calls != 1 {
		t.Errorf("summarizer calls = %d, want 1", summarizer.calls)
	}
	if len(note.errors) != 0 {
		t.Errorf("unexpected error notifications: %v", note.errors)
	}
}

func TestGenerateRetriesOnErrorThenSucceeds(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){
		func() (string, error) { return "", errors.New("boom") },
		okHighlights,
	}}
	note := &fakeNotifier{}

	result, err := newTestService(collector, summarizer, note).Generate(t.Context(), Request{GuildID: "g"})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if summarizer.calls != 2 {
		t.Errorf("summarizer calls = %d, want 2", summarizer.calls)
	}
	if !result.Publishable {
		t.Error("should succeed on retry")
	}
}

func TestGenerateRetriesOnEmptyHighlights(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){
		func() (string, error) { return "[]", nil },
	}}
	note := &fakeNotifier{}

	result, err := newTestService(collector, summarizer, note).Generate(t.Context(), Request{GuildID: "g"})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if summarizer.calls != 3 {
		t.Errorf("summarizer calls = %d, want MaxAttempts (3)", summarizer.calls)
	}
	if result.Publishable {
		t.Error("all-empty highlights should not be publishable")
	}
}

func TestGenerateExhaustsRetriesAndReportsRaw(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1, FallbackText: "フォールバック"}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){
		func() (string, error) { return "RAW_PAYLOAD not json", nil },
	}}
	note := &fakeNotifier{}

	result, err := newTestService(collector, summarizer, note).Generate(t.Context(), Request{GuildID: "g"})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if summarizer.calls != 3 {
		t.Errorf("summarizer calls = %d, want 3", summarizer.calls)
	}
	if result.Publishable {
		t.Error("should not be publishable after exhausting retries")
	}
	found := false
	for _, msg := range note.errors {
		if strings.Contains(msg, "RAW_PAYLOAD") {
			found = true
		}
	}
	if !found {
		t.Errorf("raw response should be surfaced to operators: %v", note.errors)
	}
}

func TestGenerateCollectorError(t *testing.T) {
	collector := &fakeCollector{err: errors.New("discord down")}
	note := &fakeNotifier{}

	_, err := newTestService(collector, &fakeSummarizer{}, note).Generate(t.Context(), Request{GuildID: "g"})
	if err == nil {
		t.Fatal("Generate should propagate collector error")
	}
	if len(note.errors) == 0 {
		t.Error("collector failure should notify operators")
	}
}

func TestGenerateRequiresGuildID(t *testing.T) {
	svc := newTestService(&fakeCollector{}, &fakeSummarizer{}, &fakeNotifier{})
	if _, err := svc.Generate(t.Context(), Request{}); err == nil {
		t.Error("Generate should fail without guild id")
	}
}

func TestGenerateNoMessages(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{FallbackText: "誰も喋らなかった"}}
	note := &fakeNotifier{}

	result, err := newTestService(collector, &fakeSummarizer{}, note).Generate(t.Context(), Request{GuildID: "g"})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if result.Publishable {
		t.Error("no digests should not be publishable")
	}
	if result.Content != "誰も喋らなかった" {
		t.Errorf("content = %q, want fallback text", result.Content)
	}
}

func TestGenerateReportsSkippedChannels(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1, SkippedChannels: 16}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){okHighlights}}
	note := &fakeNotifier{}

	if _, err := newTestService(collector, summarizer, note).Generate(t.Context(), Request{GuildID: "g"}); err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	found := false
	for _, msg := range note.infos {
		if strings.Contains(msg, "16件スキップ") {
			found = true
		}
	}
	if !found {
		t.Errorf("skipped channels should be reported: %v", note.infos)
	}
}

type fakePublisher struct {
	channelID string
	content   string
	embeds    int
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, channelID, content string, embeds []*discordgo.MessageEmbed) error {
	if f.err != nil {
		return f.err
	}
	f.channelID = channelID
	f.content = content
	f.embeds = len(embeds)
	return nil
}

func TestRunPublishes(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){okHighlights}}
	pub := &fakePublisher{}
	svc := newTestService(collector, summarizer, &fakeNotifier{})
	svc.Publisher = pub

	result, err := svc.Run(t.Context(), Request{GuildID: "g", ChannelID: "ch1", TargetName: "dev"})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if !result.Posted {
		t.Error("result should be posted")
	}
	if pub.channelID != "ch1" || pub.embeds != 1 || pub.content == "" {
		t.Errorf("publish args = %+v", pub)
	}
}

func TestRunSkipsWhenNotPublishable(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{FallbackText: "静かな一日"}}
	pub := &fakePublisher{}
	svc := newTestService(collector, &fakeSummarizer{}, &fakeNotifier{})
	svc.Publisher = pub

	result, err := svc.Run(t.Context(), Request{GuildID: "g", ChannelID: "ch1"})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Posted || pub.channelID != "" {
		t.Error("nothing should be published when not publishable")
	}
}

func TestRunPublishFailure(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){okHighlights}}
	svc := newTestService(collector, summarizer, &fakeNotifier{})
	svc.Publisher = &fakePublisher{err: errors.New("discord down")}

	if _, err := svc.Run(t.Context(), Request{GuildID: "g", ChannelID: "ch1"}); err == nil {
		t.Error("Run should propagate publish error")
	}
}

func TestOddityPostsEye(t *testing.T) {
	collector := &fakeCollector{result: CollectorResult{Digests: testDigests, TotalMessages: 1}}
	summarizer := &fakeSummarizer{responses: []func() (string, error){okHighlights}}
	note := &fakeNotifier{}
	svc := newTestService(collector, summarizer, note)
	svc.Oddity = func() bool { return true }

	if _, err := svc.Generate(t.Context(), Request{GuildID: "g"}); err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	eyes := 0
	for _, msg := range note.infos {
		if msg == "👁️👁️" {
			eyes++
		}
	}
	if eyes == 0 {
		t.Error("Oddity=true should post 👁️👁️")
	}
}
