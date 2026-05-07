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

	"github.com/charmbracelet/huh"
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
func fetchLatestUbuntu2404AMIs(ctx context.Context, client *http.Client) (map[string]string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	fetchCtx, cancel := context.WithTimeout(ctx, amiFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, canonicalAWSStreamsURL, nil)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // URL is the fixed Canonical simplestreams endpoint; client is injectable for tests.
	resp, err := client.Do(req)
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

type awsPrompter struct{}

func newAWSProvider() *awsPrompter {
	return &awsPrompter{}
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

func (p *awsPrompter) RunCredentialsForm(cfg *PromptedConfig, _ *autoDetectedConfig) error {
	// Default region and smallest general-purpose instance type.
	if cfg.AWSRegion == "" {
		cfg.AWSRegion = "eu-west-1"
	}
	if cfg.AWSCPInstanceType == "" {
		cfg.AWSCPInstanceType = "t3.medium"
	}

	haChoice := cfg.AWSCPReplicas != "1"

	credGroup := huh.NewGroup(
		huh.NewInput().
			Title("Access Key ID:").
			Value(&cfg.AWSAccessKeyID).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Secret Access Key:").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.AWSSecretAccessKey).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Session Token (leave empty if not needed):").
			Value(&cfg.AWSSessionToken),
	)

	if source, ok := detectAWSCredentials(); ok {
		slog.Info("Using existing AWS credentials", slog.String("source", source))
		// Hide the credential inputs — SDK will pick them up automatically.
		credGroup = credGroup.WithHide(true)
	} else {
		slog.Info("No AWS credentials found in ~/.aws — prompting")
	}

	err := huh.NewForm(
		credGroup,
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable high availability for the control plane?").
				Value(&haChoice),
		).Title("AWS credentials").Description("Step 3/4"),
	).Run()
	if err != nil {
		return err
	}

	if haChoice {
		cfg.AWSCPReplicas = "3"
	} else {
		cfg.AWSCPReplicas = "1"
	}

	// Attempt to auto-detect AMI from Canonical; fall back to a manual prompt.
	amiMap, err := fetchLatestUbuntu2404AMIs(context.Background(), http.DefaultClient)
	if err != nil {
		slog.Warn("Failed to fetch latest Ubuntu 24.04 AMI from Canonical",
			slog.Any("error", err))
	} else if ami, ok := amiMap[cfg.AWSRegion]; ok {
		cfg.AWSAMIID = ami
	}

	if cfg.AWSAMIID == "" {
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					TitleFunc(func() string {
						return fmt.Sprintf("Ubuntu 24.04 AMI ID for region %s:", cfg.AWSRegion)
					}, &cfg.AWSRegion).
					Value(&cfg.AWSAMIID).
					Validate(nonEmpty),
			).Title("AWS AMI").Description("Step 3/4 (cont.)"),
		).Run(); err != nil {
			return err
		}
	}

	// Derive the SSH key name from the deploy key file path after Step 4 fills
	// it. We set a post-process hook via the caller's expandPaths call, but the
	// key name is derived from the basename so we do it after RunCredentialsForm
	// in the PromptConfig override below.
	return nil
}

// postProcess derives AWSSSHKeyName after the Git/SSH step has populated the key path.
// Called by ConfigFromPrompt after runGitSSHForm.
func (p *awsPrompter) postProcess(cfg *PromptedConfig) {
	keyPath := cfg.KubeaidConfigDeployKeyPath
	if keyPath == "" {
		keyPath = cfg.SSHKeyPath
	}
	cfg.AWSSSHKeyName = strings.TrimSuffix(
		filepath.Base(keyPath),
		filepath.Ext(keyPath),
	)
}
