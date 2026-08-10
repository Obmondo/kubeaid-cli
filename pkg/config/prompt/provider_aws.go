// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
)

// Canonical's unauthenticated simplestreams index for released AWS images.
// The file is ~11MB, so the timeout is intentionally generous.
const (
	canonicalAWSStreamsURL = "https://cloud-images.ubuntu.com/releases/streams/v1/com.ubuntu.cloud:released:aws.json"
	ubuntuVersion          = "26.04"
	ubuntuProductAMD64     = "com.ubuntu.cloud:server:" + ubuntuVersion + ":amd64"
	ubuntuProductARM64     = "com.ubuntu.cloud:server:" + ubuntuVersion + ":arm64"
	amiFetchTimeout        = 15 * time.Second

	// The control plane defaults to ARM (Graviton) at the small
	// general-purpose sizing it always had (t4g.medium is the Graviton
	// equivalent of the old t3.medium default). Workers default to amd64 —
	// c6i.xlarge is the smallest current-gen x86 type meeting the
	// 4 vCPU / 8 GB baseline. Both are overridable in general.yaml; each gets
	// its own AMI, resolved for its own architecture, so CP and workers may
	// differ.
	defaultAWSCPInstanceType   = "t4g.medium"
	defaultAWSNodeInstanceType = "c6i.xlarge"
)

// awsARMInstanceTypeRegexp matches AWS Graviton (arm64) instance types by
// their family naming convention : the letters following the generation digit
// include 'g' (t4g, m7g, c7gn, x2gd, im4gn, g5g, hpc7g), plus the first-gen
// a1 family. The GPU families (g4dn, g5) don't match — their 'g' precedes the
// generation digit.
var awsARMInstanceTypeRegexp = regexp.MustCompile(`^(?:[a-z]+\d+[a-z]*g[a-z]*|a1)\.`)

// awsInstanceTypeIsARM reports whether the given EC2 instance type is ARM
// (Graviton) based, from its name alone — no AWS API call, so it works before
// credentials are configured.
func awsInstanceTypeIsARM(instanceType string) bool {
	return awsARMInstanceTypeRegexp.MatchString(instanceType)
}

// ubuntuProductForInstanceType returns the simplestreams product matching the
// instance type's architecture.
func ubuntuProductForInstanceType(instanceType string) string {
	if awsInstanceTypeIsARM(instanceType) {
		return ubuntuProductARM64
	}
	return ubuntuProductAMD64
}

// archForProduct returns the architecture suffix of a simplestreams product
// ID, for prompt labels.
func archForProduct(productID string) string {
	if productID == ubuntuProductARM64 {
		return "arm64"
	}
	return "amd64"
}

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

// fetchUbuntuSimplestreamsIndex pulls Canonical's published simplestreams
// index. The index covers every release + architecture product, so one fetch
// (~11MB) serves both the control-plane and worker AMI lookups.
func fetchUbuntuSimplestreamsIndex(
	ctx context.Context,
	client *http.Client,
) (*awsSimplestreamsIndex, error) {
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

	return &index, nil
}

// latestUbuntuAMIs returns the newest HVM + SSD-backed AMI IDs from the index
// for the given product (an Ubuntu release + architecture pair), keyed by
// region.
func latestUbuntuAMIs(index *awsSimplestreamsIndex, productID string) (map[string]string, error) {
	product, ok := index.Products[productID]
	if !ok {
		return nil, fmt.Errorf("product %q missing from simplestreams index", productID)
	}

	// Version keys are YYYYMMDD-style strings — lexicographic descending order gives newest first.
	versionKeys := make([]string, 0, len(product.Versions))
	for k := range product.Versions {
		versionKeys = append(versionKeys, k)
	}
	if len(versionKeys) == 0 {
		return nil, fmt.Errorf("no versions listed for product %q", productID)
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
		return nil, fmt.Errorf("no HVM/SSD AMIs found for product %q", productID)
	}

	return amis, nil
}

type awsPrompter struct{}

func newAWSProvider() *awsPrompter {
	return &awsPrompter{}
}

func (p *awsPrompter) SummaryLines(cfg *PromptedConfig) []string {
	if cfg.AWSEKS {
		return []string{
			fmt.Sprintf("  Region:        %s", cfg.AWSRegion),
			"  Control plane: EKS (managed by AWS)",
		}
	}
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
	// Default region and instance types — ARM (Graviton) by default : small
	// control plane, 4 vCPU / 8 GB workers.
	if cfg.AWSRegion == "" {
		cfg.AWSRegion = "eu-west-1"
	}
	if cfg.AWSCPInstanceType == "" {
		cfg.AWSCPInstanceType = defaultAWSCPInstanceType
	}
	if cfg.AWSNodeInstanceType == "" {
		cfg.AWSNodeInstanceType = defaultAWSNodeInstanceType
	}

	// Control-plane flavour comes BEFORE credentials : the credentials are the
	// same either way, but every question after them differs — EKS has no HA /
	// replica choice (AWS runs the control plane multi-AZ), no AMI (CAPA
	// resolves the EKS optimized AL2023 image itself) and no SSH key name.
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[bool]().
				Title("Control plane:").
				Options(
					huh.NewOption("Self-managed (EC2, kubeadm)", false),
					huh.NewOption("EKS (managed by AWS)", true),
				).
				Value(&cfg.AWSEKS),
		).Title("AWS control plane").Description("Step 3/4"),
	).Run(); err != nil {
		return err
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

	haGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable high availability for the control plane?").
			Value(&haChoice),
	).WithHideFunc(func() bool {
		// EKS control planes are HA by design — nothing to ask.
		return cfg.AWSEKS
	})

	err := huh.NewForm(
		credGroup,
		haGroup.Title("AWS credentials").Description("Step 3/4 (cont.)"),
	).Run()
	if err != nil {
		return err
	}

	if cfg.AWSEKS {
		// AWS owns the control plane and CAPA resolves worker AMIs — nothing
		// more to collect.
		return nil
	}

	if haChoice {
		cfg.AWSCPReplicas = "3"
	} else {
		cfg.AWSCPReplicas = "1"
	}

	// Attempt to auto-detect the AMIs from Canonical; fall back to manual
	// prompts. Control plane and workers each get an AMI resolved for their
	// own instance type's architecture (ARM CP + amd64 workers by default),
	// from a single index fetch.
	cpProduct := ubuntuProductForInstanceType(cfg.AWSCPInstanceType)
	nodeProduct := ubuntuProductForInstanceType(cfg.AWSNodeInstanceType)

	index, err := fetchUbuntuSimplestreamsIndex(context.Background(), http.DefaultClient)
	if err != nil {
		slog.Warn("Failed to fetch the Ubuntu "+ubuntuVersion+" AMI index from Canonical",
			slog.Any("error", err))
	} else {
		if amiMap, amiErr := latestUbuntuAMIs(index, cpProduct); amiErr != nil {
			slog.Warn("Failed resolving the control-plane AMI", slog.Any("error", amiErr))
		} else if ami, ok := amiMap[cfg.AWSRegion]; ok {
			cfg.AWSAMIID = ami
		}

		if amiMap, amiErr := latestUbuntuAMIs(index, nodeProduct); amiErr != nil {
			slog.Warn("Failed resolving the worker node-group AMI", slog.Any("error", amiErr))
		} else if ami, ok := amiMap[cfg.AWSRegion]; ok {
			cfg.AWSNodeAMIID = ami
		}
	}

	var amiInputs []huh.Field
	if cfg.AWSAMIID == "" {
		amiInputs = append(amiInputs, huh.NewInput().
			TitleFunc(func() string {
				return fmt.Sprintf(
					"Ubuntu %s AMI ID for the control plane (%s) in region %s:",
					ubuntuVersion, archForProduct(cpProduct), cfg.AWSRegion,
				)
			}, &cfg.AWSRegion).
			Value(&cfg.AWSAMIID).
			Validate(nonEmpty))
	}
	if cfg.AWSNodeAMIID == "" {
		amiInputs = append(amiInputs, huh.NewInput().
			TitleFunc(func() string {
				return fmt.Sprintf(
					"Ubuntu %s AMI ID for the worker node-group (%s) in region %s:",
					ubuntuVersion, archForProduct(nodeProduct), cfg.AWSRegion,
				)
			}, &cfg.AWSRegion).
			Value(&cfg.AWSNodeAMIID).
			Validate(nonEmpty))
	}
	if len(amiInputs) > 0 {
		if err := huh.NewForm(
			huh.NewGroup(amiInputs...).Title("AWS AMI").Description("Step 3/4 (cont.)"),
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
// EKS clusters skip it : workers stay keyless (SSM Session Manager covers
// debugging) and the rendered config must not carry an sshKeyName.
func (p *awsPrompter) postProcess(cfg *PromptedConfig) {
	if cfg.AWSEKS {
		return
	}

	keyPath := cfg.KubeaidConfigDeployKeyPath
	if keyPath == "" {
		keyPath = cfg.SSHKeyPath
	}
	cfg.AWSSSHKeyName = strings.TrimSuffix(
		filepath.Base(keyPath),
		filepath.Ext(keyPath),
	)
}
