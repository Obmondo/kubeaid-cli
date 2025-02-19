package logger

import (
	"log/slog"
	"os"
)

// Initializes the logger.
func InitLogger(isDebugModeEnabled bool) {
	logLevel := slog.LevelInfo
	if isDebugModeEnabled {
		logLevel = slog.LevelDebug
	}

	textHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,

		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("15:04"))
			}
			return a
		},
	})

	logger := slog.New(withContextualSlogAttributesHandler(textHandler))
	slog.SetDefault(logger)
}

func Error(err error) slog.Attr {
	return slog.Any("error", err)
}
