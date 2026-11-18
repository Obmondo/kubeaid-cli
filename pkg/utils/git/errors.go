// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"errors"
	"log/slog"
)

// ErrBranchExists is returned by CreateAndCheckoutToBranch when the branch already exists.
var ErrBranchExists = errors.New("branch already exists")

// Error is how this package reports a failed operation. Message is the headline a command-line
// caller prints when it gives up on the error, Err is the cause, and Attrs are the log attributes
// of the operation that failed (the repository, branch or ref it worked on), for that same line.
type Error struct {
	Message string
	Err     error
	Attrs   []slog.Attr
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

// fail wraps err with message and attrs. It returns nil for a nil err, so call sites can write
// `return fail("...", err)` after any step that produced an error value.
func fail(message string, err error, attrs ...slog.Attr) error {
	if err == nil {
		return nil
	}
	return &Error{Message: message, Err: err, Attrs: attrs}
}

// annotate adds attrs to the *Error held by *errp, if there is one. An operation that attaches
// attributes to its log context defers it, so that its failures carry those attributes out.
func annotate(errp *error, attrs ...slog.Attr) {
	var gitErr *Error
	if errors.As(*errp, &gitErr) {
		gitErr.Attrs = append(gitErr.Attrs, attrs...)
	}
}
