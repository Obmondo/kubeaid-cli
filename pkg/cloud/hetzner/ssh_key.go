// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

type GetSSHKeysResponse []struct {
	Key struct {
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
		Type        string `json:"type"`
		Size        int    `json:"size"`
		Data        string `json:"data"`
		CreatedAt   string `json:"created_at"`
	} `json:"key"`
}

func (h *Hetzner) ValidateHetznerSSHKeyPair(ctx context.Context, hetznerConfig *config.HetznerConfig) {
	keyName := hetznerConfig.BareMetal.SSHKeyPair.Name

	// directly loading up key data since it's processed during parsing phase
	publicKey := hetznerConfig.BareMetal.SSHKeyPair.PublicKey
	slog.InfoContext(ctx,
		"Checking if SSH key exists in Hetzner",
		slog.String("key_name", keyName),
	)
	h.checkAndCreateSSHKeyPair(ctx, keyName, publicKey)
}

func (h *Hetzner) checkAndCreateSSHKeyPair(
	ctx context.Context,
	keyName string,
	keyData string,
) {
	response, err := h.robotClient.R().Get("/key")
	assert.AssertErrNil(ctx, err, "Failed getting SSH keys")

	// Official docs says Error 404 means keys not found i.e. no keys are available so it's safe to ignore
	if response.StatusCode() == http.StatusNotFound {
		response = nil
	} else {
		assert.Assert(ctx,
			response.StatusCode() == http.StatusOK,
			"Failed getting SSH keys",
			slog.Any("response", response),
		)
	}

	keyExists := false

	// Check if key exists
	if response != nil {
		var getSSHKeysResponse GetSSHKeysResponse
		err = json.Unmarshal(response.Body(), &getSSHKeysResponse)
		assert.AssertErrNil(ctx, err, "Failed JSON unmarshalling GetSSHKeysResponse")

		for _, k := range getSSHKeysResponse {
			if k.Key.Name == keyName {
				keyExists = true
				slog.InfoContext(ctx,
					"SSH key already exists in Hetzner",
					slog.String("key_name", keyName),
				)
				break
			}
		}
	}

	// Create it, if not found
	if !keyExists {
		slog.InfoContext(ctx,
			"SSH key does not exist in Hetzner, creating it",
			slog.String("key_name", keyName),
		)
		createResponse, err := h.robotClient.R().
			SetFormData(map[string]string{
				"name": keyName,
				"data": keyData,
			}).
			Post("/key")

		assert.AssertErrNil(ctx, err, "Failed creating SSH key")

		// 201 = Created (from docs)
		assert.Assert(ctx,
			createResponse.StatusCode() == http.StatusCreated,
			"Failed creating SSH key",
			slog.Any("response", createResponse),
		)

		slog.InfoContext(ctx,
			"Added the SSH key pair in Hetzner",
			slog.String("key_name", keyName),
		)
	}
}
