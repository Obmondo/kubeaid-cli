// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"github.com/Obmondo/kubeaid-cli/pkg/render"
)

// PromptedConfig and Render moved to pkg/render, which is a leaf: stdlib
// plus pkg/constants and pkg/urlprotocol. The Obmondo API renders the same
// two files server-side, and importing this package to do it would have
// pulled ~2200 transitive packages and required copying kubeaid-cli's
// replace directives into its go.mod. Re-exported here so the CLI's own
// call sites — which are many — are unchanged.
//
// A type alias, not a defined type: existing code assigns, embeds and takes
// addresses of PromptedConfig across package boundaries, all of which a
// defined type would break.
type PromptedConfig = render.PromptedConfig

// Render executes the general.yaml and secrets.yaml templates against cfg
// and returns the rendered bytes. Pure — no filesystem or network access —
// so it is safe to call from contexts that must not touch disk.
// writeConfigFiles is a thin caller of Render for the CLI's own
// disk-writing path; the two must stay byte-identical.
func Render(cfg *PromptedConfig) (general []byte, secrets []byte, err error) {
	return render.Render(cfg)
}
