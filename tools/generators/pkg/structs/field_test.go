// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package structs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseValidateStructTag(t *testing.T) {
	tests := []struct {
		name         string
		tag          string
		wantRequired bool
		wantEnum     []string
	}{
		{name: "empty tag", tag: "", wantRequired: false, wantEnum: nil},
		{name: "notblank", tag: "notblank", wantRequired: true, wantEnum: nil},
		{name: "required", tag: "required", wantRequired: true, wantEnum: nil},
		{name: "required with gt", tag: "required,gt=100", wantRequired: true, wantEnum: nil},
		{
			name:         "notblank with oneof",
			tag:          "notblank,oneof=vpn workload",
			wantRequired: true,
			wantEnum:     []string{"vpn", "workload"},
		},
		{
			name:         "notblank with three-way oneof",
			tag:          "notblank,oneof=bare-metal hcloud hybrid",
			wantRequired: true,
			wantEnum:     []string{"bare-metal", "hcloud", "hybrid"},
		},
		{name: "oneof without required or notblank", tag: "oneof=cloudflare", wantRequired: false, wantEnum: []string{"cloudflare"}},
		{name: "omitempty alone", tag: "omitempty,email", wantRequired: false, wantEnum: nil},
		{name: "omitempty with fqdn alternation", tag: "omitempty,fqdn|hostname_rfc1123", wantRequired: false, wantEnum: nil},
		{name: "omitempty with oneof", tag: "omitempty,oneof=tcp udp", wantRequired: false, wantEnum: []string{"tcp", "udp"}},
		{name: "plain format rule, no requiredness signal", tag: "cidrv4", wantRequired: false, wantEnum: nil},
		{
			// The field itself may be empty (omitempty precedes dive);
			// "required" after dive governs slice elements, not the field.
			name:         "omitempty,dive,required — field itself is optional",
			tag:          "omitempty,dive,required",
			wantRequired: false,
			wantEnum:     nil,
		},
		{
			// "required" before dive governs the field; "notblank" after
			// dive governs elements and must not leak into the field's
			// own Required (it already is true here, but must not add
			// an enum or otherwise be misread as the field's own rule).
			name:         "required,min=1,dive,notblank — field is required",
			tag:          "required,min=1,dive,notblank",
			wantRequired: true,
			wantEnum:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRequired, gotEnum := parseValidateStructTag(tc.tag)
			assert.Equal(t, tc.wantRequired, gotRequired)
			assert.Equal(t, tc.wantEnum, gotEnum)
		})
	}
}
