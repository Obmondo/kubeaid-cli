// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/logger"
)

// Creates the given SSH key in HCloud, if it doesn't already exist.
func (h *Hetzner) CreateHCloudSSHKey(ctx context.Context, name string, sshKeyPair config.SSHKeyPairConfig) {
	// List all the HCloud SSH keys.
	sshKeys, response, err := h.hcloudClient.SSHKey.List(ctx, hcloud.SSHKeyListOpts{})
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusOK)),
		"Failed listing HCloud SSH keys",
		logger.Error(err), slog.Any("response", response),
	)

	for _, sshKey := range sshKeys {
		switch {
		// Check whether we have an SSH key with different name but same data.
		case (sshKey.Fingerprint == sshKeyPair.Fingerprint):
			assert.Assert(ctx, (sshKey.Name == name),
				"Found an HCloud SSH key with different name but same fingerprint")

		// Check whether we have an SSH key with same name but different data.
		case (sshKey.Name == name):
			assert.Assert(ctx, (sshKey.Fingerprint == sshKeyPair.Fingerprint),
				"Found an HCloud SSH key with same name but different fingerprint")

		default:
			continue
		}

		slog.InfoContext(ctx, "HCloud SSH key already exists")
		return
	}

	// We need to create the HCloud SSH key.
	_, response, err = h.hcloudClient.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      name,
		PublicKey: sshKeyPair.PublicKey,
	})
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode == http.StatusCreated)),
		"Failed creating HCloud SSH key",
		logger.Error(err), slog.Any("response", response),
	)
	slog.InfoContext(ctx, "Created HCloud SSH key")
}

type (
	GetKeysResponse []struct {
		Key Key `json:"key"`
	}

	Key struct {
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
	}
)

// Creates the given SSH key in Hetzner Bare Metal, if it doesn't already exist.
func (h *Hetzner) CreateHetznerBareMetalSSHKey(
	ctx context.Context,
	name string,
	sshKeyPair config.SSHKeyPairConfig,
) {
	// Query all the SSH keys.

	response, err := h.robotClient.R().Get("/key")
	assert.AssertErrNil(ctx, err, "Failed getting Hetzner Bare Metal SSH keys")

	switch response.StatusCode() {
	// No SSH keys exist.
	case http.StatusNotFound:
		break

	case http.StatusOK:
		// Check whether the SSH key already exists.

		var getKeysResponse GetKeysResponse
		err = json.Unmarshal(response.Body(), &getKeysResponse)
		assert.AssertErrNil(ctx, err, "Failed JSON unmarshalling GetKeysResponse")

		for _, element := range getKeysResponse {
			key := element.Key

			switch {
			// Check whether we have an SSH key with different name but same fingerprint.
			case (key.Fingerprint == sshKeyPair.Fingerprint):
				assert.Assert(ctx, (key.Name == name),
					"Found a Hetzner Bare Metal SSH key with different name but same fingerprint")

			// Check whether we have an SSH key with same name but different fingerprint.
			case (key.Name == name):
				assert.Assert(ctx, (key.Fingerprint == sshKeyPair.Fingerprint),
					"Found a Hetzner Bare Metal SSH key with same name but different fingerprint")

			default:
				continue
			}

			slog.InfoContext(ctx, "Hetzner Bare Metal SSH key already exists")
			return
		}

	default:
		slog.ErrorContext(ctx,
			"Unexpected response statuscode, when trying to list Hetzner Bare Metal SSH keys",
			logger.Error(err), slog.Any("response", response),
		)
		os.Exit(1)
	}

	// No SSH keys exist.
	// Let's create this one.

	response, err = h.robotClient.R().
		SetFormData(map[string]string{
			"name": name,
			"data": sshKeyPair.PublicKey,
		}).
		Post("/key")
	assert.Assert(ctx,
		((err == nil) && (response.StatusCode() == http.StatusCreated)),
		"Failed creating Hetzner Bare Metal SSH key",
		logger.Error(err), slog.Any("response", response),
	)

	slog.InfoContext(ctx, "Created SSH key in Hetzner Bare Metal")
}
