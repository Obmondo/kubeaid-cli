package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"

	"github.com/fatih/color"
)

type ColorHandler struct {
	slog.Handler
	logLogger          *log.Logger
	isDebugModeEnabled bool
}

func (c *ColorHandler) Handle(ctx context.Context, record slog.Record) error {
	logParts := []any{}

	// Time (only shown in debug mode).
	if c.isDebugModeEnabled {
		logParts = append(logParts,
			fmt.Sprintf("(%s)", record.Time.Format("15:04")),
		)
	}

	// Log level.
	logLevel := record.Level.String() + " :"
	switch record.Level {
	case slog.LevelDebug:
		logLevel = color.MagentaString(logLevel)
	case slog.LevelInfo:
		logLevel = color.GreenString(logLevel)
	case slog.LevelWarn:
		logLevel = color.YellowString(logLevel)
	case slog.LevelError:
		logLevel = color.RedString(logLevel)
	}
	logParts = append(logParts, logLevel)

	// Message.
	message := color.WhiteString(record.Message)
	logParts = append(logParts, message)

	// Attributes.
	record.Attrs(func(attribute slog.Attr) bool {
		logParts = append(logParts,
			fmt.Sprintf("%s=%s", color.CyanString(attribute.Key), attribute.Value.String()),
		)

		return true
	})

	c.logLogger.Println(logParts...)
	return nil
}

func withColorHandler(out io.Writer, handler slog.Handler, isDebugModeEnabled bool) *ColorHandler {
	return &ColorHandler{
		Handler:            handler,
		logLogger:          log.New(out, "", 0),
		isDebugModeEnabled: isDebugModeEnabled,
	}
}
