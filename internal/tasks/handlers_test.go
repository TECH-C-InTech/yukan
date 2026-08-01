package tasks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"

	"yukan/internal/config"
	"yukan/internal/summary"
)

type fakeService struct {
	result summary.Result
	err    error
	gotReq summary.Request
}

func (f *fakeService) Generate(_ context.Context, req summary.Request) (summary.Result, error) {
	f.gotReq = req
	return f.result, f.err
}

type fakeSender struct {
	headers []string
	embeds  []*discordgo.MessageEmbed
	err     error
}

func (f *fakeSender) ChannelMessageSend(_ string, content string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.headers = append(f.headers, content)
	return &discordgo.Message{}, nil
}

func (f *fakeSender) ChannelMessageSendComplex(_ string, data *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.embeds = append(f.embeds, data.Embeds...)
	return &discordgo.Message{}, nil
}

func newTestHandler(service *fakeService, sender *fakeSender) *Handler {
	return &Handler{
		service: service,
		sender:  sender,
		targets: map[string]config.Target{
			"dev": {GuildID: "g", SourceGuildID: "sg", ChannelID: "c"},
		},
		defaultTarget: "dev",
	}
}

func post(t *testing.T, h *Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleDailySummary(rec, httptest.NewRequest(http.MethodPost, url, nil))
	return rec
}

func TestDailySummaryMethodNotAllowed(t *testing.T) {
	h := newTestHandler(&fakeService{}, &fakeSender{})
	rec := httptest.NewRecorder()
	h.handleDailySummary(rec, httptest.NewRequest(http.MethodGet, "/tasks/daily-summary", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d, want 405", rec.Code)
	}
}

func TestDailySummaryUnknownTarget(t *testing.T) {
	h := newTestHandler(&fakeService{}, &fakeSender{})
	if rec := post(t, h, "/tasks/daily-summary?target=nope"); rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

func TestDailySummaryGenerateError(t *testing.T) {
	h := newTestHandler(&fakeService{err: errors.New("boom")}, &fakeSender{})
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rec.Code)
	}
}

func TestDailySummaryEmptyContent(t *testing.T) {
	h := newTestHandler(&fakeService{result: summary.Result{Content: "  "}}, &fakeSender{})
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusNoContent {
		t.Errorf("code = %d, want 204", rec.Code)
	}
}

func TestDailySummaryNotPublishable(t *testing.T) {
	sender := &fakeSender{}
	h := newTestHandler(&fakeService{result: summary.Result{Content: "本文", Publishable: false}}, sender)
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusNoContent {
		t.Errorf("code = %d, want 204", rec.Code)
	}
	if len(sender.headers) != 0 || len(sender.embeds) != 0 {
		t.Error("nothing should be posted when not publishable")
	}
}

func TestDailySummaryPosts(t *testing.T) {
	service := &fakeService{result: summary.Result{
		Content:     "[**夕刊**] 2026年7月31日",
		Embeds:      []*discordgo.MessageEmbed{{Title: "a"}, {Title: "b"}, nil},
		Publishable: true,
	}}
	sender := &fakeSender{}
	h := newTestHandler(service, sender)

	rec := post(t, h, "/tasks/daily-summary?target=dev")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if len(sender.headers) != 1 {
		t.Errorf("headers = %d, want 1", len(sender.headers))
	}
	// リアクション文化のため embed は1つずつ別メッセージで送る
	if len(sender.embeds) != 2 {
		t.Errorf("embeds = %d, want 2 (nil is skipped)", len(sender.embeds))
	}
	if service.gotReq.GuildID != "sg" {
		t.Errorf("GuildID = %q, want source guild", service.gotReq.GuildID)
	}
}

func TestDailySummaryPostFailure(t *testing.T) {
	service := &fakeService{result: summary.Result{Content: "c", Publishable: true}}
	h := newTestHandler(service, &fakeSender{err: errors.New("discord down")})
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rec.Code)
	}
}

func TestNewMuxNilService(t *testing.T) {
	mux := NewMux(Config{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tasks/daily-summary", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("code = %d, want 503", rec.Code)
	}
}
