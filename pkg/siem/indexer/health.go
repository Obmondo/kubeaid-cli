// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package indexer

import (
	"context"
	"net/http"
	"strconv"
)

// percent converts a used/total ratio.
const percent = 100

// Health is an indexer's cluster status and its fullest data node.
type Health struct {
	// Status is green, yellow or red.
	Status string `json:"status"`
	// DiskUsedPercent is the highest disk usage of any data node, the
	// figure the flood-stage watermark (95 % by default) is checked on.
	DiskUsedPercent float64 `json:"diskUsedPercent"`
	// DiskUsedBytes / DiskTotalBytes are summed over the data nodes.
	DiskUsedBytes  float64 `json:"diskUsedBytes"`
	DiskTotalBytes float64 `json:"diskTotalBytes"`
	Nodes          int     `json:"nodes"`
}

// Health reads GET /_cluster/health and GET /_cat/allocation. Both are
// cheap (cluster-state lookups) and need only the cluster monitor
// permission.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	var ch struct {
		Status        string `json:"status"`
		NumberOfNodes int    `json:"number_of_nodes"`
	}
	if err := c.call(ctx, http.MethodGet, "/_cluster/health", nil, &ch); err != nil {
		return h, err
	}
	h.Status, h.Nodes = ch.Status, ch.NumberOfNodes

	var alloc []map[string]*string
	if err := c.call(ctx, http.MethodGet, "/_cat/allocation?format=json&bytes=b", nil, &alloc); err != nil {
		return h, err
	}
	for _, row := range alloc {
		used, uok := number(row["disk.used"])
		total, tok := number(row["disk.total"])
		if !uok || !tok || total <= 0 {
			continue // UNASSIGNED row
		}
		h.DiskUsedBytes += used
		h.DiskTotalBytes += total
		h.DiskUsedPercent = max(h.DiskUsedPercent, used/total*percent)
	}
	return h, nil
}

func number(s *string) (float64, bool) {
	if s == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(*s, 64)
	return v, err == nil
}
