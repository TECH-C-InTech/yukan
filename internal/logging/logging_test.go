package logging

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSeverityMapping(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelWarn, "WARNING"},
		{slog.LevelError, "ERROR"},
	}
	for _, tt := range tests {
		if got := severity(tt.level); got != tt.want {
			t.Errorf("severity(%v) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestHTTPMiddlewareInjectsTrace(t *testing.T) {
	var gotCtx context.Context
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotCtx = r.Context()
	})
	handler := HTTPMiddleware("my-project", inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Cloud-Trace-Context", "abc123/456;o=1")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	trace, _ := gotCtx.Value(traceKey).(string)
	if trace != "projects/my-project/traces/abc123" {
		t.Errorf("trace = %q", trace)
	}
}

func TestHTTPMiddlewareWithoutHeader(t *testing.T) {
	var gotCtx context.Context
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotCtx = r.Context()
	})
	HTTPMiddleware("my-project", inner).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if v := gotCtx.Value(traceKey); v != nil {
		t.Errorf("trace should be absent, got %v", v)
	}
}
