package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/fatih/color"
)

type CustomTextHandler struct {
	writer  io.Writer
	options *slog.HandlerOptions
}

func NewCustomTextHandler(writer io.Writer, options *slog.HandlerOptions) *CustomTextHandler {
	return &CustomTextHandler{
		writer,
		options,
	}
}

func (c *CustomTextHandler) Enabled(_ context.Context, logLevel slog.Level) bool {
	return (logLevel >= c.options.Level.Level())
}

func (c *CustomTextHandler) Handle(_ context.Context, record slog.Record) error {
	logSections := []string{}

	// Time.
	logSections = append(logSections,
		fmt.Sprintf("(%s)", record.Time.Format("15:04")),
	)

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
	logSections = append(logSections, logLevel)

	// Message.
	message := color.WhiteString(record.Message)
	logSections = append(logSections, message)

	// Attributes.
	record.Attrs(func(attribute slog.Attr) bool {
		logSections = append(logSections,
			fmt.Sprintf("%s=%s", color.CyanString(attribute.Key), attribute.Value.String()),
		)

		return true
	})

	// Write out the log.
	log := strings.Join(logSections, " ") + "\n"
	_, _ = c.writer.Write([]byte(log))

	return nil
}

func (c *CustomTextHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	panic("unimplemented")
}

func (c *CustomTextHandler) WithGroup(_ string) slog.Handler {
	panic("unimplemented")
}
