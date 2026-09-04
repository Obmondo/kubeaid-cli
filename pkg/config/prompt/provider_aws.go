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
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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

// resolveUbuntuAMI extracts the region's newest AMI for the given product
// from an already-fetched index. Returns "" when the index is unavailable,
// the product can't be resolved (warned, target names which AMI it was), or
// the region has no image — the caller falls back to a manual prompt.
func resolveUbuntuAMI(index *awsSimplestreamsIndex, productID, region, target string) string {
	if index == nil {
		return ""
	}

	amiMap, err := latestUbuntuAMIs(index, productID)
	if err != nil {
		slog.Warn("Failed resolving the "+target+" AMI", slog.Any("error", err))
		return ""
	}

	return amiMap[region]
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
	var lines []string

	// Only shown when a profile was actually chosen — an empty one means the
	// SDK's default resolution, which there is nothing to report about.
	if cfg.AWSProfile != "" {
		lines = append(lines, fmt.Sprintf("  Profile:       %s", cfg.AWSProfile))
	}

	if cfg.AWSEKS {
		return append(lines,
			fmt.Sprintf("  Region:        %s", cfg.AWSRegion),
			"  Control plane: EKS (managed by AWS)",
		)
	}

	return append(lines,
		fmt.Sprintf("  Region:        %s", cfg.AWSRegion),
		fmt.Sprintf("  Instance type: %s", cfg.AWSCPInstanceType),
		fmt.Sprintf("  CP replicas:   %s", cfg.AWSCPReplicas),
	)
}

// awsSharedCredentialsFilePath and awsSharedConfigFilePath return the two
// ~/.aws file locations, honouring the SDK's overrides so a direnv-style setup
// pointing elsewhere is still read.
func awsSharedCredentialsFilePath() string {
	if override := os.Getenv(constants.EnvNameAWSSharedCredentialsFile); override != "" {
		return override
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".aws", "credentials")
}

func awsSharedConfigFilePath() string {
	if override := os.Getenv(constants.EnvNameAWSConfigFile); override != "" {
		return override
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".aws", "config")
}

// awsINISectionRegexp captures the body of an ini section header.
var awsINISectionRegexp = regexp.MustCompile(`^\s*\[([^\]]+)\]`)

// parseAWSProfileNames extracts the profile names from one ~/.aws file.
// In config, profiles are "[profile foo]" plus a bare "[default]", and the
// other section kinds (sso-session, services) are not profiles; in
// credentials, every section "[foo]" is a profile.
func parseAWSProfileNames(contents string, isConfigFile bool) []string {
	var names []string

	for _, line := range strings.Split(contents, "\n") {
		match := awsINISectionRegexp.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		section := strings.TrimSpace(match[1])

		if isConfigFile {
			if section != "default" && !strings.HasPrefix(section, "profile ") {
				continue
			}
			section = strings.TrimSpace(strings.TrimPrefix(section, "profile "))
		}

		// Profile names containing spaces are quoted in the ini files.
		section = strings.Trim(section, `"'`)

		if section != "" {
			names = append(names, section)
		}
	}

	return names
}

// detectAWSProfiles lists the profiles configured across ~/.aws/config and
// ~/.aws/credentials, sorted and de-duplicated.
func detectAWSProfiles() []string {
	seen := map[string]bool{}
	profiles := []string{}

	for _, file := range []struct {
		path     string
		isConfig bool
	}{
		{awsSharedConfigFilePath(), true},
		{awsSharedCredentialsFilePath(), false},
	} {
		if file.path == "" {
			continue
		}

		contents, err := os.ReadFile(file.path)
		if err != nil {
			continue
		}

		for _, name := range parseAWSProfileNames(string(contents), file.isConfig) {
			if seen[name] {
				continue
			}
			seen[name] = true
			profiles = append(profiles, name)
		}
	}

	sort.Strings(profiles)

	return profiles
}

// preferredAWSProfile picks which profile the selection prompt starts on :
// whatever AWS_PROFILE already names, else "default", else the first listed.
func preferredAWSProfile(profiles []string) string {
	if len(profiles) == 0 {
		return ""
	}

	for _, candidate := range []string{os.Getenv(constants.EnvNameAWSProfile), "default"} {
		if candidate != "" && slices.Contains(profiles, candidate) {
			return candidate
		}
	}

	return profiles[0]
}

// prefillAWSCredentialsFromEnv seeds the credential inputs from the SDK's own
// environment variables, so an operator who already has them exported confirms
// them instead of retyping. Only blanks are filled : values recovered from an
// existing secrets.yaml are the operator's stated choice and win.
//
// They are prefilled rather than accepted silently because env vars live for
// one shell session, while `cluster bootstrap` needs the credentials on every
// later run — so they have to reach secrets.yaml.
func prefillAWSCredentialsFromEnv(cfg *PromptedConfig) {
	before := cfg.AWSAccessKeyID

	cfg.AWSAccessKeyID = firstNonEmpty(
		cfg.AWSAccessKeyID, os.Getenv(constants.EnvNameAWSAccessKey),
	)
	cfg.AWSSecretAccessKey = firstNonEmpty(
		cfg.AWSSecretAccessKey, os.Getenv(constants.EnvNameAWSSecretKey),
	)
	cfg.AWSSessionToken = firstNonEmpty(
		cfg.AWSSessionToken, os.Getenv(constants.EnvNameAWSSessionToken),
	)

	if before == "" && cfg.AWSAccessKeyID != "" {
		slog.Info("Prefilled the AWS credentials from the environment — confirm them to store them")
	}
}

// manualAWSCredentialsChoice is the sentinel index the profile select uses for
// "type the keys in instead". Profile options carry their index in the list,
// so it can't collide with one.
const manualAWSCredentialsChoice = -1

// chooseAWSProfile settles where this cluster's AWS credentials come from,
// before anything else asks for them.
//
// It returns whether the credential inputs still need to be shown, seeding them
// from the environment when they do. cfg.AWSProfile is left empty when the
// SDK's own default resolution is the right answer — nothing configured, or a
// lone "default" profile.
//
// A config never ends up carrying both a profile and keys : at bootstrap the
// keys would win, so the profile would be a lie.
func chooseAWSProfile(cfg *PromptedConfig) (promptForKeys bool, err error) {
	profiles := detectAWSProfiles()

	switch {
	case len(profiles) == 0:
		// Nothing in ~/.aws to pick from. Drop any profile carried over from a
		// previous run — it no longer resolves to anything.
		slog.Info("No AWS profiles found in ~/.aws — prompting for credentials")
		cfg.AWSProfile = ""
		prefillAWSCredentialsFromEnv(cfg)

		return true, nil

	case len(profiles) == 1:
		// One profile, no question to ask. It gets named only when it would
		// actually be used : not when the config already carries keys (they
		// win), and not when it is "default" (which the SDK resolves anyway).
		if profiles[0] != "default" && cfg.AWSAccessKeyID == "" {
			cfg.AWSProfile = profiles[0]
		}
		slog.Info("Using the only AWS profile found in ~/.aws",
			slog.String("profile", profiles[0]),
		)

		return false, nil
	}

	options := make([]huh.Option[int], 0, len(profiles)+1)
	for i, profile := range profiles {
		options = append(options, huh.NewOption(profile, i))
	}
	options = append(options,
		huh.NewOption("Enter credentials manually", manualAWSCredentialsChoice),
	)

	selected := slices.Index(profiles, firstNonEmpty(cfg.AWSProfile, preferredAWSProfile(profiles)))
	if selected < 0 {
		selected = 0
	}

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("AWS profile to use for this cluster:").
				Description("Found several profiles in ~/.aws — the chosen one is stored in secrets.yaml.").
				Options(options...).
				Value(&selected),
		).Title("AWS credentials").Description("Step 3/4 (cont.)"),
	).Run(); err != nil {
		return false, err
	}

	if selected == manualAWSCredentialsChoice {
		cfg.AWSProfile = ""
		prefillAWSCredentialsFromEnv(cfg)

		return true, nil
	}

	cfg.AWSProfile = profiles[selected]
	// The chosen profile is now the source of truth, so keys carried over from
	// a previous run must go — they would otherwise win over it at bootstrap.
	cfg.AWSAccessKeyID = ""
	cfg.AWSSecretAccessKey = ""
	cfg.AWSSessionToken = ""

	return false, nil
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

	// Where the credentials come from is settled before we decide whether to
	// ask for keys : on a machine carrying several AWS accounts, the SDK's
	// default profile is rarely the account this cluster belongs to.
	promptForKeys, err := chooseAWSProfile(cfg)
	if err != nil {
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
	).WithHide(!promptForKeys)

	haGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable high availability for the control plane?").
			Value(&haChoice),
	).WithHideFunc(func() bool {
		// EKS control planes are HA by design — nothing to ask.
		return cfg.AWSEKS
	})

	err = huh.NewForm(
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

	cfg.AWSCPReplicas = "1"
	if haChoice {
		cfg.AWSCPReplicas = "3"
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
	}

	if ami := resolveUbuntuAMI(index, cpProduct, cfg.AWSRegion, "control-plane"); ami != "" {
		cfg.AWSAMIID = ami
	}
	if ami := resolveUbuntuAMI(index, nodeProduct, cfg.AWSRegion, "worker node-group"); ami != "" {
		cfg.AWSNodeAMIID = ami
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
