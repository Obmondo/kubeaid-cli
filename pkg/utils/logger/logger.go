package logger

import (
	"log/slog"
	"os"

	"github.com/go-logr/logr/slogr"
	controllerRuntimeLogger "sigs.k8s.io/controller-runtime/pkg/log"
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

	// Initialize controller-runtime's (or kubebuilder's) base logger with the default slog logger.
	// REFER : https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/log.
	controllerRuntimeLogger.SetLogger(slogr.NewLogr(slog.Default().Handler()))
}

func Error(err error) slog.Attr {
	return slog.Any("error", err)
}
