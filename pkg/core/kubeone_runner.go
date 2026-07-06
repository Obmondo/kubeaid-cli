// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	kubeoneCmd "k8c.io/kubeone/pkg/cmd"

	"github.com/Obmondo/kubeaid-cli/pkg/globals"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/progress"
)

// kubeoneLogLine matches KubeOne's logrus text lines - 'INFO[10:28:22 IST] Installing kubeadm...'.
// Everything else (remote script dumps, kubeadm -v=6 output, the plan block) goes to the log
// file only.
var kubeoneLogLine = regexp.MustCompile(`^(INFO|WARN|ERRO)\[[^\]]*\]\s+(.*)$`)

// runKubeOne runs the embedded KubeOne root command, taming its output : every byte lands in
// this run's log file (the one root.go opened under outputs/logs/), while the terminal gets
// one de-duplicated line per KubeOne task (plus WARN / ERRO lines). On failure the last
// captured lines are replayed, with a pointer to the log. --debug bypasses the capture
// entirely and adds KubeOne's own --verbose flag.
func runKubeOne(ctx context.Context, logLabel string, kubeoneArgs ...string) error {
	if globals.IsDebugModeEnabled {
		return executeKubeOne(ctx, append(kubeoneArgs, "--verbose"))
	}

	logFile := io.Writer(io.Discard)
	if globals.LogFile != nil {
		logFile = globals.LogFile
	}
	_, _ = fmt.Fprintf(logFile, "----- kubeone %s : output start -----\n", logLabel)
	defer func() { _, _ = fmt.Fprintf(logFile, "----- kubeone %s : output end -----\n", logLabel) }()

	bar := progress.FromCtx(ctx)
	bar.Substep("Streaming KubeOne output to " + globals.LogFilePath)

	// KubeOne's logrus logger and its plan block bind os.Stderr / os.Stdout when they run -
	// swapping both routes everything into the pipe. Our own writers are immune : the progress
	// bar and the slog logger captured the real handles at construction. The spinner is paused
	// anyway so the streamed task lines don't fight it for the terminal line.
	realStdout, realStderr := os.Stdout, os.Stderr
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating pipe for KubeOne output: %w", err)
	}

	bar.Pause()
	os.Stdout, os.Stderr = pipeWriter, pipeWriter

	tail := make([]string, 0, kubeoneErrorTailLines)
	scannerDone := make(chan struct{})
	go func() {
		defer close(scannerDone)

		lastShown := ""
		scanner := bufio.NewScanner(pipeReader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = fmt.Fprintln(logFile, line)

			if len(tail) == kubeoneErrorTailLines {
				tail = append(tail[1:], line)
			} else {
				tail = append(tail, line)
			}

			level, message, ok := parseKubeOneLogLine(line)
			if !ok || (message == lastShown) {
				continue
			}
			lastShown = message

			prefix := "    · "
			if level != "INFO" {
				prefix = "    ! "
			}
			_, _ = fmt.Fprintln(realStderr, prefix+message)
		}
	}()

	executionErr := executeKubeOne(ctx, kubeoneArgs)

	os.Stdout, os.Stderr = realStdout, realStderr
	pipeWriter.Close()
	<-scannerDone
	pipeReader.Close()
	bar.Resume()

	if executionErr != nil {
		_, _ = fmt.Fprintf(realStderr, "\nLast %d lines of KubeOne output :\n\n", len(tail))
		for _, line := range tail {
			_, _ = fmt.Fprintln(realStderr, "  "+line)
		}
		_, _ = fmt.Fprintf(realStderr, "\nFull KubeOne log : %s\n\n", globals.LogFilePath)
	}
	return executionErr
}

const kubeoneErrorTailLines = 30

func executeKubeOne(ctx context.Context, kubeoneArgs []string) error {
	kubeoneRootCmd := kubeoneCmd.NewRoot()
	kubeoneRootCmd.SetArgs(kubeoneArgs)
	return kubeoneRootCmd.ExecuteContext(ctx)
}

// parseKubeOneLogLine extracts the level and (field-stripped) message from a KubeOne logrus
// text line. KubeOne pads the message to a fixed width before appending 'key=value' fields -
// splitting on the first run of 2+ spaces drops them.
func parseKubeOneLogLine(line string) (level, message string, ok bool) {
	match := kubeoneLogLine.FindStringSubmatch(line)
	if match == nil {
		return "", "", false
	}

	message = match[2]
	if i := strings.Index(message, "  "); i > 0 {
		message = message[:i]
	}
	return match[1], strings.TrimSpace(message), true
}
