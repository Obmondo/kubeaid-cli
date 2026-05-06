// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package hetzner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hetznercloud/hcloud-go/hcloud"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
)

// CreateHCloudSSHKey creates the given SSH key in HCloud, if it doesn't already exist.
func (h *Hetzner) CreateHCloudSSHKey(ctx context.Context, name string, sshKeyPair config.SSHKeyPairConfig) error {
	sshKeys, response, err := h.hcloudClient.SSHKey.List(ctx, hcloud.SSHKeyListOpts{})
	if err != nil {
		return fmt.Errorf("listing HCloud SSH keys: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("listing HCloud SSH keys: unexpected status %d", response.StatusCode)
	}

	for _, sshKey := range sshKeys {
		switch {
		case sshKey.Fingerprint == sshKeyPair.Fingerprint:
			if sshKey.Name != name {
				return fmt.Errorf("found an HCloud SSH key with different name but same fingerprint")
			}

		case sshKey.Name == name:
			if sshKey.Fingerprint != sshKeyPair.Fingerprint {
				return fmt.Errorf("found an HCloud SSH key with same name but different fingerprint")
			}

		default:
			continue
		}

		slog.InfoContext(ctx, "HCloud SSH key already exists")
		return nil
	}

	_, response, err = h.hcloudClient.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      name,
		PublicKey: sshKeyPair.PublicKey,
	})
	if err != nil {
		return fmt.Errorf("creating HCloud SSH key: %w", err)
	}
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("creating HCloud SSH key: unexpected status %d", response.StatusCode)
	}
	slog.InfoContext(ctx, "Created HCloud SSH key")

	return nil
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

// CreateHetznerBareMetalSSHKey creates the given SSH key in Hetzner Bare Metal, if it doesn't already exist.
func (h *Hetzner) CreateHetznerBareMetalSSHKey(
	ctx context.Context,
	name string,
	sshKeyPair config.SSHKeyPairConfig,
) error {
	response, err := h.robotClient.R().Get("/key")
	if err != nil {
		return fmt.Errorf("getting Hetzner Bare Metal SSH keys: %w", err)
	}

	switch response.StatusCode() {
	case http.StatusNotFound:
		// No SSH keys exist.

	case http.StatusOK:
		var getKeysResponse GetKeysResponse
		if err = json.Unmarshal(response.Body(), &getKeysResponse); err != nil {
			return fmt.Errorf("unmarshalling keys response: %w", err)
		}

		for _, element := range getKeysResponse {
			key := element.Key

			switch {
			case key.Fingerprint == sshKeyPair.Fingerprint:
				if key.Name != name {
					return fmt.Errorf("found a Hetzner Bare Metal SSH key with different name but same fingerprint")
				}

			case key.Name == name:
				if key.Fingerprint != sshKeyPair.Fingerprint {
					return fmt.Errorf("found a Hetzner Bare Metal SSH key with same name but different fingerprint")
				}

			default:
				continue
			}

			slog.InfoContext(ctx, "Hetzner Bare Metal SSH key already exists")
			return nil
		}

	default:
		return fmt.Errorf("unexpected response status code %d when listing Hetzner Bare Metal SSH keys", response.StatusCode())
	}

	response, err = h.robotClient.R().
		SetFormData(map[string]string{
			"name": name,
			"data": sshKeyPair.PublicKey,
		}).
		Post("/key")
	if err != nil {
		return fmt.Errorf("creating Hetzner Bare Metal SSH key: %w", err)
	}
	if response.StatusCode() != http.StatusCreated {
		return fmt.Errorf("creating Hetzner Bare Metal SSH key: unexpected status %d", response.StatusCode())
	}

	slog.InfoContext(ctx, "Created SSH key in Hetzner Bare Metal")

	return nil
}
