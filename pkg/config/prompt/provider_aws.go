// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Canonical's unauthenticated simplestreams index for released AWS images.
// The file is ~11MB, so the timeout is intentionally generous.
const (
	canonicalAWSStreamsURL = "https://cloud-images.ubuntu.com/releases/streams/v1/com.ubuntu.cloud:released:aws.json"
	ubuntu2404ProductID    = "com.ubuntu.cloud:server:24.04:amd64"
	amiFetchTimeout        = 15 * time.Second
)

// simplestreams JSON structures — see https://cloudinit.readthedocs.io/en/latest/topics/datasources/simplestreams.html
// and Canonical's published schema. Only fields we care about are decoded.
type (
	awsSimplestreamsIndex struct {
		Products map[string]awsSimplestreamsProduct `json:"products"`
	}

	awsSimplestreamsProduct struct {
		Versions map[string]awsSimplestreamsVersion `json:"versions"`
	}

	awsSimplestreamsVersion struct {
		Items map[string]awsSimplestreamsItem `json:"items"`
	}

	awsSimplestreamsItem struct {
		ID string `json:"id"`
		// CRSN is the AWS region code in the current schema (e.g. "eu-west-1").
		// Region is the legacy field kept for forward/backward compatibility.
		CRSN      string `json:"crsn"`
		Region    string `json:"region"`
		RootStore string `json:"root_store"`
		Virt      string `json:"virt"`
	}
)

// fetchLatestUbuntu2404AMIs pulls Canonical's published simplestreams index
// and returns the newest HVM + SSD-backed AMI IDs keyed by region.
func fetchLatestUbuntu2404AMIs(ctx context.Context) (map[string]string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, amiFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, canonicalAWSStreamsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"unexpected status %s fetching %s",
			resp.Status,
			canonicalAWSStreamsURL,
		)
	}

	var index awsSimplestreamsIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("decoding simplestreams index: %w", err)
	}

	product, ok := index.Products[ubuntu2404ProductID]
	if !ok {
		return nil, fmt.Errorf("product %q missing from simplestreams index", ubuntu2404ProductID)
	}

	// Version keys are YYYYMMDD-style strings — lexicographic descending order gives newest first.
	versionKeys := make([]string, 0, len(product.Versions))
	for k := range product.Versions {
		versionKeys = append(versionKeys, k)
	}
	if len(versionKeys) == 0 {
		return nil, fmt.Errorf("no versions listed for product %q", ubuntu2404ProductID)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versionKeys)))

	latest := product.Versions[versionKeys[0]]

	amis := make(map[string]string, len(latest.Items))
	for _, item := range latest.Items {
		if item.Virt != "hvm" {
			continue
		}
		// Canonical emits SSD and instance-store variants with store names like
		// "ssd", "ssd-gp2", "ssd-gp3", "ssd-io1", etc. Match any SSD-backed variant.
		if !strings.HasPrefix(item.RootStore, "ssd") {
			continue
		}
		region := item.CRSN
		if region == "" {
			region = item.Region
		}
		if region == "" {
			continue
		}
		if _, exists := amis[region]; !exists {
			amis[region] = item.ID
		}
	}

	if len(amis) == 0 {
		return nil, fmt.Errorf("no HVM/SSD AMIs found for product %q", ubuntu2404ProductID)
	}

	return amis, nil
}

type awsPrompter struct {
	baseProvider
}

func newAWSProvider() *awsPrompter {
	p := &awsPrompter{}
	p.questionsFunc = p.promptAWSQuestions
	return p
}

// PromptConfig overrides the base flow to derive SSH key name after promptSSHAuth runs.
func (p *awsPrompter) PromptConfig(cfg *PromptedConfig, detected *autoDetectedConfig) error {
	if err := p.baseProvider.PromptConfig(cfg, detected); err != nil {
		return err
	}

	// Derive the SSH key name from the deploy key file path
	// (e.g. "/home/user/.ssh/id_ed25519" → "id_ed25519").
	keyPath := cfg.KubeaidConfigDeployKeyPath
	if keyPath == "" {
		keyPath = cfg.SSHKeyPath
	}
	cfg.AWSSSHKeyName = strings.TrimSuffix(
		filepath.Base(keyPath),
		filepath.Ext(keyPath),
	)

	return nil
}

func (p *awsPrompter) SummaryLines(cfg *PromptedConfig) []string {
	return []string{
		fmt.Sprintf("  Region:        %s", cfg.AWSRegion),
		fmt.Sprintf("  Instance type: %s", cfg.AWSCPInstanceType),
		fmt.Sprintf("  CP replicas:   %s", cfg.AWSCPReplicas),
	}
}

// detectAWSCredentials reports whether AWS credentials are reachable via
// ~/.aws files. On success it also returns the path where they were found.
func detectAWSCredentials() (source string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	for _, candidate := range []string{
		filepath.Join(home, ".aws", "credentials"),
		filepath.Join(home, ".aws", "config"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}

	return "", false
}

func (p *awsPrompter) promptAWSQuestions(cfg *PromptedConfig) error {
	// Only prompt for credentials if none are discoverable under ~/.aws.
	// Otherwise the SDK picks them up automatically.
	if source, ok := detectAWSCredentials(); ok {
		slog.Info("Using existing AWS credentials", slog.String("source", source))
	} else {
		slog.Info("No AWS credentials found in ~/.aws — prompting")

		if err := requiredInput("Access Key ID:", &cfg.AWSAccessKeyID); err != nil {
			return err
		}

		if err := requiredPassword("Secret Access Key:", &cfg.AWSSecretAccessKey); err != nil {
			return err
		}

		if err := optionalInput(
			"Session Token (leave empty if not needed):",
			"", &cfg.AWSSessionToken,
		); err != nil {
			return err
		}
	}

	// Default region and smallest general-purpose instance type.
	cfg.AWSRegion = "eu-west-1"
	cfg.AWSCPInstanceType = "t3.medium"

	amiMap, err := fetchLatestUbuntu2404AMIs(context.Background())
	if err != nil {
		return fmt.Errorf("fetching latest Ubuntu 24.04 AMIs from Canonical: %w", err)
	}
	ami, ok := amiMap[cfg.AWSRegion]
	if !ok {
		return fmt.Errorf(
			"no Ubuntu 24.04 AMI for region %s in Canonical's published index",
			cfg.AWSRegion,
		)
	}
	cfg.AWSAMIID = ami

	replicas, err := promptHAControlPlane()
	if err != nil {
		return err
	}
	cfg.AWSCPReplicas = replicas

	return nil
}
