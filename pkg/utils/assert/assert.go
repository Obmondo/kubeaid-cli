package assert

import (
	"context"
	"log/slog"
	"os"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Panics if the given error isn't nil.
func AssertErrNil(ctx context.Context, err error, customErrorMessage string, attributes ...any) {
	if err == nil {
		return
	}

	attributes = append(attributes, logger.Error(err))
	slog.ErrorContext(ctx, customErrorMessage, attributes...)
	os.Exit(1)
}

// Panics if the given value isn't nil.
func AssertNil(ctx context.Context, value interface{}, errorMessage string, attributes ...any) {
	if value == nil {
		return
	}

	slog.ErrorContext(ctx, errorMessage, attributes...)
	os.Exit(1)
}

// Panics if the given value is nil.
func AssertNotNil(ctx context.Context, value interface{}, errorMessage string, attributes ...any) {
	if value != nil {
		return
	}

	slog.ErrorContext(ctx, errorMessage, attributes...)
	os.Exit(1)
}

// Panics if the given value is false.
func Assert(ctx context.Context, value bool, errorMessage string, attributes ...any) {
	if value {
		return
	}

	slog.ErrorContext(ctx, errorMessage, attributes...)
	os.Exit(1)
}
