// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/giturl"
)

func TestGetRepoDir(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantHost  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "github HTTPS URL with .git suffix",
			url:       "https://github.com/Obmondo/kubeaid-bootstrap-script.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-bootstrap-script",
		},
		{
			name:      "github SCP-style SSH URL",
			url:       "git@github.com:Obmondo/kubeaid-config.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-config",
		},
		{
			name:      "github HTTPS without .git suffix",
			url:       "https://github.com/foo/bar",
			wantHost:  "github.com",
			wantOwner: "foo",
			wantRepo:  "bar",
		},
		{
			name:      "self-hosted gitea on custom SSH port",
			url:       "ssh://git@gitea.obmondo.com:2223/EnableIT/kubeaid-config-enableit.git",
			wantHost:  "gitea.obmondo.com:2223",
			wantOwner: "EnableIT",
			wantRepo:  "kubeaid-config-enableit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := giturl.Parse(tc.url)
			require.NoError(t, err)

			got := GetRepoDir(parsed)
			want := path.Join(constants.TempDirectory, tc.wantHost, tc.wantOwner, tc.wantRepo)
			assert.Equal(t, want, got)
		})
	}
}
