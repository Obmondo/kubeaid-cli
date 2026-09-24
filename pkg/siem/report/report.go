// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package report holds the per-object result type every SIEM
// reconciler component returns, and the table printer.
package report

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// Action is what happened (or, in dry-run mode, would happen) to one
// object.
type Action string

// Actions.
const (
	ActionOK     Action = "ok"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	// ActionSkip marks an object the reconciler looked at but will not
	// manage (e.g. a missing IRIS service account).
	ActionSkip  Action = "skip"
	ActionError Action = "error"
)

// Result is one reconciled object. Detail never carries secret values.
type Result struct {
	Component string
	Kind      string
	Name      string
	Action    Action
	Detail    string
}

// IsChange reports whether the result is a create or update.
func (r Result) IsChange() bool { return r.Action == ActionCreate || r.Action == ActionUpdate }

// Summary counts results.
type Summary struct {
	Changes int
	Errors  int
}

// Summarize counts changes and errors.
func Summarize(results []Result) Summary {
	var s Summary
	for _, r := range results {
		switch {
		case r.IsChange():
			s.Changes++
		case r.Action == ActionError:
			s.Errors++
		}
	}
	return s
}

// Print writes the "component kind name action detail" table and the
// final "N changes" line.
func Print(w io.Writer, results []Result, dryRun bool) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "COMPONENT\tKIND\tNAME\tACTION\tDETAIL"); err != nil {
		return err
	}
	for _, r := range results {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Component, r.Kind, r.Name, r.Action, r.Detail); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	s := Summarize(results)
	mode := "applied"
	if dryRun {
		mode = "dry run, nothing written"
	}
	_, err := fmt.Fprintf(w, "%d changes, %d errors (%s)\n", s.Changes, s.Errors, mode)
	return err
}
