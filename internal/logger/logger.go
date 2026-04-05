package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
)

type contextKey string

const (
	keyModule  contextKey = "module"
	keyLogCode contextKey = "log_code"
)

func Init(level string) {
	lvl := parseLevel(level)
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(time.Now().UTC().Format(time.RFC3339Nano))
			}
			return a
		},
	})
	slog.SetDefault(slog.New(h))
}

func WithModule(ctx context.Context, module string) context.Context {
	return context.WithValue(ctx, keyModule, module)
}

func WithLogCode(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, keyLogCode, code)
}

func Info(ctx context.Context, msg string, args ...any) {
	slog.InfoContext(ctx, msg, withContext(ctx, args)...)
}

func Warn(ctx context.Context, msg string, args ...any) {
	slog.WarnContext(ctx, msg, withContext(ctx, args)...)
}

func Error(ctx context.Context, msg string, args ...any) {
	slog.ErrorContext(ctx, msg, withContext(ctx, args)...)
}

func Debug(ctx context.Context, msg string, args ...any) {
	slog.DebugContext(ctx, msg, withContext(ctx, args)...)
}

func withContext(ctx context.Context, args []any) []any {
	if m, ok := ctx.Value(keyModule).(string); ok {
		args = append(args, "module", m)
	}
	if c, ok := ctx.Value(keyLogCode).(string); ok {
		args = append(args, "log_code", c)
	}
	return args
}

func parseLevel(s string) slog.Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARNING", "WARN":
		return slog.LevelWarn
	case "ERROR", "CRITICAL":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
