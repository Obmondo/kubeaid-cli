// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package wazuh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AgentSummary is the agent connection summary of a manager.
type AgentSummary struct {
	Active         int `json:"active"`
	Disconnected   int `json:"disconnected"`
	NeverConnected int `json:"never_connected"`
	Pending        int `json:"pending"`
	Total          int `json:"total"`
}

// AgentSummary reads GET /agents/summary/status (agent:read).
func (c *Client) AgentSummary(ctx context.Context) (AgentSummary, error) {
	var data struct {
		Connection AgentSummary `json:"connection"`
	}
	err := c.call(ctx, http.MethodGet, "/agents/summary/status", nil, &data)
	return data.Connection, err
}

// AnalysisdStats is the event loss of the manager's analysis engine
// since the daemon started.
type AnalysisdStats struct {
	// EventsDropped counts the events analysisd discarded: queue-full
	// drops of every source plus the EPS-limit drops.
	EventsDropped float64 `json:"eventsDropped"`
	// QueueUsageMax is the fullest analysisd queue, as the API reports
	// it (percent).
	QueueUsageMax float64 `json:"queueUsageMax"`
}

// AnalysisdStats reads GET /manager/daemons/stats for wazuh-analysisd
// (manager:read).
func (c *Client) AnalysisdStats(ctx context.Context) (AnalysisdStats, error) {
	var s AnalysisdStats
	type item struct {
		Name    string `json:"name"`
		Metrics struct {
			EPS struct {
				EventsDropped float64 `json:"events_dropped"`
			} `json:"eps"`
			Events struct {
				ReceivedBreakdown struct {
					DroppedBreakdown json.RawMessage `json:"dropped_breakdown"`
				} `json:"received_breakdown"`
			} `json:"events"`
			Queues map[string]struct {
				Usage float64 `json:"usage"`
			} `json:"queues"`
		} `json:"metrics"`
	}
	list, err := items[item](ctx, c, "/manager/daemons/stats?daemons_list=wazuh-analysisd")
	if err != nil {
		return s, err
	}
	for _, it := range list {
		if it.Name != "" && it.Name != "wazuh-analysisd" {
			continue
		}
		dropped, err := sumLeaves(it.Metrics.Events.ReceivedBreakdown.DroppedBreakdown)
		if err != nil {
			return s, fmt.Errorf("decoding analysisd dropped_breakdown: %w", err)
		}
		s.EventsDropped += dropped + it.Metrics.EPS.EventsDropped
		for _, q := range it.Metrics.Queues {
			s.QueueUsageMax = max(s.QueueUsageMax, q.Usage)
		}
	}
	return s, nil
}

// sumLeaves adds every number in a nested JSON object.
func sumLeaves(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	var walk func(any) float64
	walk = func(v any) float64 {
		switch t := v.(type) {
		case float64:
			return t
		case map[string]any:
			sum := 0.0
			for _, x := range t {
				sum += walk(x)
			}
			return sum
		}
		return 0
	}
	return walk(v), nil
}
