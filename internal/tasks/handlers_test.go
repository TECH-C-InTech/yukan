package tasks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"yukan/internal/config"
	"yukan/internal/summary"
)

type fakeService struct {
	result summary.Result
	err    error
	gotReq summary.Request
}

func (f *fakeService) Run(_ context.Context, req summary.Request) (summary.Result, error) {
	f.gotReq = req
	return f.result, f.err
}

func newTestHandler(service *fakeService) *Handler {
	return &Handler{
		service: service,
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
	h := newTestHandler(&fakeService{})
	rec := httptest.NewRecorder()
	h.handleDailySummary(rec, httptest.NewRequest(http.MethodGet, "/tasks/daily-summary", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d, want 405", rec.Code)
	}
}

func TestDailySummaryUnknownTarget(t *testing.T) {
	h := newTestHandler(&fakeService{})
	if rec := post(t, h, "/tasks/daily-summary?target=nope"); rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

func TestDailySummaryGenerateError(t *testing.T) {
	h := newTestHandler(&fakeService{err: errors.New("boom")})
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rec.Code)
	}
}

func TestDailySummaryEmptyContent(t *testing.T) {
	h := newTestHandler(&fakeService{result: summary.Result{Content: "  "}})
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusNoContent {
		t.Errorf("code = %d, want 204", rec.Code)
	}
}

func TestDailySummaryNotPosted(t *testing.T) {
	h := newTestHandler(&fakeService{result: summary.Result{Content: "本文", Publishable: false, Posted: false}})
	if rec := post(t, h, "/tasks/daily-summary"); rec.Code != http.StatusNoContent {
		t.Errorf("code = %d, want 204", rec.Code)
	}
}

func TestDailySummaryPosts(t *testing.T) {
	service := &fakeService{result: summary.Result{
		Content:     "[**夕刊**] 2026年7月31日",
		Publishable: true,
		Posted:      true,
	}}
	h := newTestHandler(service)

	rec := post(t, h, "/tasks/daily-summary?target=dev")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if service.gotReq.GuildID != "sg" {
		t.Errorf("GuildID = %q, want source guild", service.gotReq.GuildID)
	}
	if service.gotReq.ChannelID != "c" {
		t.Errorf("ChannelID = %q, want target channel", service.gotReq.ChannelID)
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
