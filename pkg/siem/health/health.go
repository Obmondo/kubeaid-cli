// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

// Package health holds the platform health the siem-reconciler reads
// after each run in long-running mode: tenant and central indexer
// status and disk, manager agent counts and event loss, and component
// reachability. It is exported as Prometheus metrics and as the JSON
// /status summary. Error strings never carry secret values.
package health

import "time"

// Snapshot is one health probe of the platform.
type Snapshot struct {
	Time       time.Time   `json:"time"`
	Indexers   []Indexer   `json:"indexers,omitempty"`
	Managers   []Manager   `json:"managers,omitempty"`
	Components []Component `json:"components,omitempty"`
}

// Indexer is one OpenSearch cluster: a tenant code or "central".
type Indexer struct {
	Name            string  `json:"name"`
	Up              bool    `json:"up"`
	Error           string  `json:"error,omitempty"`
	Status          string  `json:"status,omitempty"`
	DiskUsedPercent float64 `json:"diskUsedPercent"`
	DiskUsedBytes   float64 `json:"diskUsedBytes"`
	DiskTotalBytes  float64 `json:"diskTotalBytes"`
}

// Manager is one tenant's Wazuh manager.
type Manager struct {
	Tenant string `json:"tenant"`
	Up     bool   `json:"up"`
	Error  string `json:"error,omitempty"`

	AgentsActive         int `json:"agentsActive"`
	AgentsDisconnected   int `json:"agentsDisconnected"`
	AgentsNeverConnected int `json:"agentsNeverConnected"`
	AgentsPending        int `json:"agentsPending"`
	AgentsTotal          int `json:"agentsTotal"`

	// StatsAvailable is false when the analysisd stats could not be
	// read (the agent counts may still be valid).
	StatsAvailable bool    `json:"statsAvailable"`
	EventsDropped  float64 `json:"eventsDropped"`
	QueueUsageMax  float64 `json:"queueUsageMax"`
}

// Component is the reachability of a central component's API.
type Component struct {
	Name  string `json:"name"`
	Up    bool   `json:"up"`
	Error string `json:"error,omitempty"`
}

// Healthy reports whether everything probed is up and no indexer is red.
func (s Snapshot) Healthy() bool {
	for _, i := range s.Indexers {
		if !i.Up || i.Status == "red" {
			return false
		}
	}
	for _, m := range s.Managers {
		if !m.Up {
			return false
		}
	}
	for _, c := range s.Components {
		if !c.Up {
			return false
		}
	}
	return true
}
