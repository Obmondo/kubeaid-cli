// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package git

import (
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/giturl"
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
			url:       "https://github.com/Obmondo/kubeaid-cli.git",
			wantHost:  "github.com",
			wantOwner: "Obmondo",
			wantRepo:  "kubeaid-cli",
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
			// Custom SSH port — must end up in the on-disk path
			// without the colon, since docker -v <src>:<dst> would
			// otherwise read the port as a third path component.
			name:      "self-hosted forge on custom SSH port",
			url:       "ssh://git@git.example.com:2223/acme/kubeaid-config-acme.git",
			wantHost:  "git.example.com",
			wantOwner: "acme",
			wantRepo:  "kubeaid-config-acme",
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
