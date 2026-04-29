// Copyright 2025 Obmondo
// SPDX-License-Identifier: AGPL3

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Returns value of the given environment variable.
// Panics if the environment variable isn't found.
func MustGetEnv(name string) string {
	value, found := os.LookupEnv(name)
	if !found || len(value) == 0 {
		slog.Error("Env not found", slog.String("name", name))
		os.Exit(1)
	}

	return value
}

// Sets value of the given environment variable.
// Panics on error.
func MustSetEnv(name, value string) {
	err := os.Setenv(name, value)
	assert.AssertErrNil(context.Background(), err,
		"Failed setting environment variable",
		slog.String("name", name),
	)
}

func WithRetry(delay time.Duration, attempts uint8, fn func() error) error {
	if attempts == 0 {
		return fmt.Errorf("WithRetry called with 0 attempts")
	}

	var err error

	for i := range attempts {
		err = fn()
		if err == nil {
			return nil
		}

		if i < (attempts - 1) { // We don't need to sleep after the last attempt.
			time.Sleep(delay)
		}
	}

	return err
}

// FetchJSON performs an HTTP GET and decodes the JSON response into the provided destination.
func FetchJSON(url string, dest any) error {
	body, err := FetchURLBytes(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

// fetchURLBytes performs an HTTP GET and returns the raw response body bytes.
func FetchURLBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
