// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package iris

import (
	"context"
	"net/http"
)

// Ping checks that IRIS answers and accepts the API key (GET /api/ping).
func (c *Client) Ping(ctx context.Context) error {
	return c.call(ctx, http.MethodGet, "/api/ping", nil, nil)
}
