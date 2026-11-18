// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

// Client runs git operations for the cluster described by a config.Config. It reads the Git and
// Forks settings from it (CA bundle, known hosts, SSH identity, kubeaid-config fork).
type Client struct {
	cfg *config.Config
}

// New returns a Client for cfg.
func New(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

// general returns the cluster's general config; an empty one when the Client has none, so that
// the accessors below never dereference nil.
func (c *Client) general() *config.GeneralConfig {
	if c == nil || c.cfg == nil || c.cfg.General == nil {
		return &config.GeneralConfig{}
	}
	return c.cfg.General
}

func (c *Client) caBundle() []byte {
	return c.general().Git.CABundle
}
