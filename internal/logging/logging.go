// Package logging は Cloud Logging 互換の slog JSON ハンドラと
// リクエストトレースの伝播を提供する。
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

type contextKey struct{}

var traceKey contextKey

// NewHandler は Cloud Logging が severity・trace として解釈できる
// JSON ログハンドラを返す。
func NewHandler() slog.Handler {
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.LevelKey:
				a.Key = "severity"
				if level, ok := a.Value.Any().(slog.Level); ok {
					a.Value = slog.StringValue(severity(level))
				}
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	})
	return traceHandler{Handler: jsonHandler}
}

// traceHandler は context に格納されたトレース ID をログレコードに添付する。
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if trace, ok := ctx.Value(traceKey).(string); ok && trace != "" {
		record.AddAttrs(slog.String("logging.googleapis.com/trace", trace))
	}
	return h.Handler.Handle(ctx, record)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{Handler: h.Handler.WithGroup(name)}
}

func severity(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARNING"
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

// HTTPMiddleware は X-Cloud-Trace-Context ヘッダーからトレース ID を
// context に載せ、以降のログをリクエスト単位で追跡可能にする。
func HTTPMiddleware(projectID string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("X-Cloud-Trace-Context")
		if traceID, _, ok := strings.Cut(header, "/"); ok && traceID != "" && projectID != "" {
			ctx := context.WithValue(r.Context(), traceKey, fmt.Sprintf("projects/%s/traces/%s", projectID, traceID))
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
