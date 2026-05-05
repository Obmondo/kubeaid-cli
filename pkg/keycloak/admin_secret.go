// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

// Package keycloak holds helpers for managing a managed-mode
// Keycloak instance from kubeaid-cli — admin credentials, and
// (in subsequent work) realm / client reconciliation via the
// admin API.
package keycloak

// GenerateAdminPassword returns a fresh random Keycloak admin
// password. The caller templates it into the keycloak-admin
// SealedSecret rendered into the kubeaid-config repo; the
// sealed-secrets controller decrypts it on first sync and the
// resulting cluster Secret is what the keycloakx chart consumes.
//
// On cluster recovery the SealedSecret in git plus the
// sealed-secrets backup of the controller's master keys are
// sufficient to recreate the cluster Secret automatically.
func GenerateAdminPassword() (string, error) {
	return generatePassword()
}
