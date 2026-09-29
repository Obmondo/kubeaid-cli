// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package parser

import (
	"errors"
	"fmt"
	"os"

	"github.com/creasty/defaults"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/config"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/git"
	"github.com/Obmondo/kubeaid-cli/pkg/utils/randval"
)

// LoadSecurityOperationsConfig reads only what the security operations files
// need from a general.yaml: forkURLs and cluster.securityOperations. Unlike
// ParseConfigFiles it needs no secrets.yaml, no cloud credentials and no API
// lookups, so it works on the kubeaid-cli.general.yaml copy in a cluster's
// kubeaid-config directory. Used by `kubeaid-cli siem render`.
func LoadSecurityOperationsConfig(generalConfigPath string) error {
	raw, err := os.ReadFile(generalConfigPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", generalConfigPath, err)
	}

	parsed := &config.GeneralConfig{}
	//nolint:musttag // Same struct and decoding as ParseConfigFiles.
	if err := yaml.Unmarshal(raw, parsed); err != nil {
		return fmt.Errorf("parsing %s: %w", generalConfigPath, err)
	}
	if err := defaults.Set(parsed); err != nil {
		return fmt.Errorf("setting defaults for %s: %w", generalConfigPath, err)
	}

	forks := &parsed.Forks
	if forks.KubeaidFork.URL == "" || forks.KubeaidConfigFork.URL == "" {
		return errors.New("forkURLs.kubeaid.url and forkURLs.kubeaidConfig.url are required")
	}
	if forks.KubeaidConfigFork.Directory == "" {
		forks.KubeaidConfigFork.Directory = parsed.Cluster.Name
	}
	if forks.KubeaidFork.ParsedURL, err = git.ParseURL(forks.KubeaidFork.URL); err != nil {
		return fmt.Errorf("parsing forkURLs.kubeaid.url: %w", err)
	}
	if forks.KubeaidConfigFork.ParsedURL, err = git.ParseURL(forks.KubeaidConfigFork.URL); err != nil {
		return fmt.Errorf("parsing forkURLs.kubeaidConfig.url: %w", err)
	}

	config.GeneralConfigFileContents = raw
	config.ParsedGeneralConfig = parsed
	if config.ParsedSecretsConfig == nil {
		config.ParsedSecretsConfig = &config.SecretsConfig{}
	}

	if !config.SecurityOperationsEnabled() {
		return fmt.Errorf("cluster.securityOperations is not enabled in %s", generalConfigPath)
	}
	hydrateSecurityOperationsDefaults()
	if err := validateSecurityOperationsConfig(); err != nil {
		return fmt.Errorf("cluster.securityOperations: %w", err)
	}
	return nil
}

// NewWazuhCredentials returns fresh logins for one Wazuh release, with the
// bcrypt hashes the chart's security config needs. Tenant releases also get
// the manager API password, the enrolment password and the cluster key.
func NewWazuhCredentials(tenant bool) (config.SecurityOperationsWazuhCredentials, error) {
	var creds config.SecurityOperationsWazuhCredentials
	var err error

	if creds.IndexerPassword, err = randval.Password(); err != nil {
		return creds, err
	}
	if creds.DashboardPassword, err = randval.Password(); err != nil {
		return creds, err
	}
	if tenant {
		if creds.APIPassword, err = wazuhAPIPassword(); err != nil {
			return creds, err
		}
		if creds.AuthdPassword, err = randval.Password(); err != nil {
			return creds, err
		}
		if creds.ClusterKey, err = randval.Password(); err != nil {
			return creds, err
		}
	}

	hash := func(password string) (string, error) {
		h, err := bcrypt.GenerateFromPassword([]byte(password), wazuhPasswordHashCost)
		return string(h), err
	}
	if creds.IndexerPasswordHash, err = hash(creds.IndexerPassword); err != nil {
		return creds, err
	}
	if creds.DashboardPasswordHash, err = hash(creds.DashboardPassword); err != nil {
		return creds, err
	}
	return creds, nil
}
