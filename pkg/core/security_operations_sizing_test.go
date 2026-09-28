// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
)

func TestSecurityOperationsIndexerStorageSize(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tenant config.SecurityOperationsTenant
		want   string
	}{
		"derived": {
			config.SecurityOperationsTenant{RetentionDays: 90, ExpectedGBPerDay: 0.5}, "68Gi",
		},
		"default retention when unset": {
			config.SecurityOperationsTenant{ExpectedGBPerDay: 0.5}, "274Gi",
		},
		"floored at the minimum": {
			config.SecurityOperationsTenant{RetentionDays: 7, ExpectedGBPerDay: 0.1}, "10Gi",
		},
		"unset GB per day falls back to the default, not to the floor": {
			config.SecurityOperationsTenant{RetentionDays: 90}, "68Gi",
		},
		"explicit override wins": {
			config.SecurityOperationsTenant{RetentionDays: 365, ExpectedGBPerDay: 5, IndexerStorageSize: "42Gi"}, "42Gi",
		},
	} {
		assert.Equal(t, tc.want, securityOperationsIndexerStorageSize(tc.tenant), name)
	}
}
