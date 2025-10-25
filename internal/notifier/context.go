package notifier

import "context"

type contextKey string

const targetKey contextKey = "notifier-target"

// WithTarget stores the target name for downstream notifiers.
func WithTarget(ctx context.Context, target string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, targetKey, target)
}

// TargetFromContext extracts the target name if present.
func TargetFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value := ctx.Value(targetKey); value != nil {
		if target, ok := value.(string); ok {
			return target
		}
	}
	return ""
}
