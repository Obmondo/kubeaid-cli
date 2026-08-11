// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"golang.org/x/crypto/ssh"
)

const (
	// The control plane keeps the small general-purpose default it always
	// had; workers default to the compute-optimised 4 vCPU / 8 GB size —
	// the Azure equivalent of the AWS default's c6i.xlarge. Both are
	// overridable in general.yaml.
	defaultAzureCPVMSize   = "Standard_B2s"
	defaultAzureNodeVMSize = "Standard_F4s_v2"

	// Default file locations for the two RSA keys a self-managed cluster
	// needs : the VM login key (Azure only accepts RSA at VM creation) and
	// the workload-identity OIDC signing key pair.
	defaultAzureSSHPublicKeyPath  = "~/.ssh/id_rsa.pub"
	defaultAzureOIDCIssuerKeyPath = "~/.ssh/azure-oidc-issuer"

	// Microsoft Entra ID token + Graph endpoints for the service-principal
	// object-id autofill.
	azureADTokenURLFormat = "https://login.microsoftonline.com/%s/oauth2/v2.0/token" //nolint:gosec // OAuth2 endpoint URL, not a credential.
	msGraphSPURLFormat    = "https://graph.microsoft.com/v1.0/servicePrincipals(appId='%s')?$select=id"
	graphLookupTimeout    = 10 * time.Second
)

type azurePrompter struct{}

func newAzureProvider() *azurePrompter {
	return &azurePrompter{}
}

func (p *azurePrompter) SummaryLines(cfg *PromptedConfig) []string {
	if cfg.AzureAKS {
		return []string{
			fmt.Sprintf("  Location:      %s", cfg.AzureLocation),
			"  Control plane: AKS (managed by Azure)",
		}
	}
	return []string{
		fmt.Sprintf("  Location:      %s", cfg.AzureLocation),
		fmt.Sprintf("  VM size:       %s", cfg.AzureCPVMSize),
		fmt.Sprintf("  Disk size:     %s GB", cfg.AzureCPDiskSizeGB),
		fmt.Sprintf("  CP replicas:   %s", cfg.AzureCPReplicas),
	}
}

// rsaPublicKeyFromPrivateKeyFile derives the authorized_keys line of the
// given private key file's public half — but only when the key is RSA (the
// only type Azure accepts at VM creation). Returns "" for any other key
// type, an unreadable file, or an encrypted/agent-held key; the caller then
// falls back to prompting.
func rsaPublicKeyFromPrivateKeyFile(path string) string {
	if path == "" {
		return ""
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	signer, err := ssh.ParsePrivateKey(contents)
	if err != nil {
		return ""
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoRSA {
		return ""
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
}

// postProcess resolves the VM SSH public key after the Git/SSH step has
// populated the deploy key path — mirroring AWS, where the deploy key names
// the EC2 key pair. When the deploy key is RSA, its public half is reused
// and no question is asked; otherwise (ed25519 deploy key, agent-held or
// encrypted key) an RSA public key file is prompted for, since Azure only
// accepts RSA keys at VM creation. AKS clusters skip it : nodes stay keyless.
func (p *azurePrompter) postProcess(cfg *PromptedConfig) error {
	if cfg.AzureAKS || cfg.AzureSSHPublicKey != "" {
		return nil
	}

	deployKeyPath := cfg.KubeaidConfigDeployKeyPath
	if deployKeyPath == "" {
		deployKeyPath = cfg.SSHKeyPath
	}
	if material := rsaPublicKeyFromPrivateKeyFile(deployKeyPath); material != "" {
		cfg.AzureSSHPublicKey = material
		return nil
	}

	sshPublicKeyPath := defaultAzureSSHPublicKeyPath
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("VM SSH public key file (RSA):").
				Description("Provisioned onto every VM. Azure only accepts RSA keys,\nand the deploy key isn't one — point at an RSA public key.").
				Value(&sshPublicKeyPath).
				Validate(validateRSAPublicKeyFile),
		).Title("Azure VM SSH key").Description("Step 4/4 (cont.)"),
	).Run(); err != nil {
		return err
	}

	// The VM SSH key travels as material (an authorized_keys line), not as
	// a path — validated RSA and readable by the input's Validate above.
	sshPublicKey, err := os.ReadFile(expandTilde(sshPublicKeyPath))
	if err != nil {
		return fmt.Errorf("reading the VM SSH public key: %w", err)
	}
	cfg.AzureSSHPublicKey = strings.TrimSpace(string(sshPublicKey))
	return nil
}

// validateRSAPublicKeyFile reports whether path points at a readable
// authorized_keys style RSA public key. Azure rejects non-RSA keys at VM
// creation time, so catching an ed25519 key here beats a mid-bootstrap ARM
// error.
func validateRSAPublicKeyFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("required")
	}
	contents, err := os.ReadFile(expandTilde(path))
	if err != nil {
		return fmt.Errorf("reading the file: %w", err)
	}
	publicKey, _, _, _, err := ssh.ParseAuthorizedKey(contents)
	if err != nil {
		return fmt.Errorf("not an SSH public key: %w", err)
	}
	if publicKey.Type() != ssh.KeyAlgoRSA {
		return fmt.Errorf("azure VMs only accept RSA keys, got %s", publicKey.Type())
	}
	return nil
}

// validateOIDCIssuerKeyPair reports whether path points at a readable private
// key with its .pub sibling beside it — the pair the workload-identity OIDC
// provider signs and publishes service-account tokens with.
func validateOIDCIssuerKeyPair(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("required")
	}
	expanded := expandTilde(path)
	if _, err := os.Stat(expanded); err != nil {
		return fmt.Errorf("reading the private key: %w", err)
	}
	if _, err := os.Stat(expanded + ".pub"); err != nil {
		return fmt.Errorf("reading the public key (expected beside it): %w", err)
	}
	return nil
}

// fetchAADServicePrincipalObjectID resolves the service principal's object id
// for the given app (client) id via Microsoft Graph, authenticating with the
// client credentials the operator just supplied. Plain HTTP on purpose — the
// two calls don't justify a Graph SDK dependency in this package.
func fetchAADServicePrincipalObjectID(
	ctx context.Context,
	client *http.Client,
	tenantID, clientID, clientSecret string,
) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}

	lookupCtx, cancel := context.WithTimeout(ctx, graphLookupTimeout)
	defer cancel()

	// Client-credentials token with the Graph .default scope.
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"scope":         {"https://graph.microsoft.com/.default"},
	}
	tokenReq, err := http.NewRequestWithContext(
		lookupCtx,
		http.MethodPost,
		fmt.Sprintf(azureADTokenURLFormat, url.PathEscape(tenantID)),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return "", err
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s fetching the Entra ID token", tokenResp.Status)
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("decoding the Entra ID token response: %w", err)
	}

	spReq, err := http.NewRequestWithContext(
		lookupCtx,
		http.MethodGet,
		fmt.Sprintf(msGraphSPURLFormat, url.QueryEscape(clientID)),
		nil,
	)
	if err != nil {
		return "", err
	}
	spReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	spResp, err := client.Do(spReq)
	if err != nil {
		return "", err
	}
	defer spResp.Body.Close()
	if spResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %s fetching the service principal", spResp.Status)
	}

	var servicePrincipal struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(spResp.Body).Decode(&servicePrincipal); err != nil {
		return "", fmt.Errorf("decoding the service principal response: %w", err)
	}
	if servicePrincipal.ID == "" {
		return "", errors.New("service principal response carries no id")
	}
	return servicePrincipal.ID, nil
}

func (p *azurePrompter) RunCredentialsForm(cfg *PromptedConfig, _ *autoDetectedConfig) error {
	// Default location, VM sizes, and disk size.
	if cfg.AzureLocation == "" {
		cfg.AzureLocation = "westeurope"
	}
	if cfg.AzureCPVMSize == "" {
		cfg.AzureCPVMSize = defaultAzureCPVMSize
	}
	if cfg.AzureNodeVMSize == "" {
		cfg.AzureNodeVMSize = defaultAzureNodeVMSize
	}
	if cfg.AzureCPDiskSizeGB == "" {
		cfg.AzureCPDiskSizeGB = "128"
	}

	// Control-plane flavour comes BEFORE credentials : the credentials are the
	// same either way, but every question after them differs — AKS has no HA /
	// replica choice (Azure runs the control plane), no storage account (it
	// only hosts the self-managed workload-identity OIDC provider), and no
	// SSH / OIDC-issuer keys.
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[bool]().
				Title("Control plane:").
				Options(
					huh.NewOption("Self-managed (Azure VMs, kubeadm)", false),
					huh.NewOption("AKS (managed by Azure)", true),
				).
				Value(&cfg.AzureAKS),
		).Title("Azure control plane").Description("Step 3/4"),
	).Run(); err != nil {
		return err
	}

	haChoice := cfg.AzureCPReplicas != "1"
	oidcIssuerKeyPath := defaultAzureOIDCIssuerKeyPath
	if cfg.AzureOIDCIssuerKeyPath != "" {
		oidcIssuerKeyPath = cfg.AzureOIDCIssuerKeyPath
	}

	credGroup := huh.NewGroup(
		huh.NewInput().
			Title("Tenant ID:").
			Value(&cfg.AzureTenantID).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Subscription ID:").
			Value(&cfg.AzureSubscriptionID).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Client ID:").
			Value(&cfg.AzureClientID).
			Validate(nonEmpty),
		huh.NewInput().
			Title("Client Secret:").
			EchoMode(huh.EchoModePassword).
			Value(&cfg.AzureClientSecret).
			Validate(nonEmpty),
	)

	selfManagedGroup := huh.NewGroup(
		huh.NewConfirm().
			Title("Enable high availability for the control plane?").
			Value(&haChoice),
		huh.NewInput().
			Title("Workload-identity OIDC signing key (RSA private key file):").
			Description("Signs the cluster's ServiceAccount tokens; its .pub feeds the JWKS document.\nGenerate with : ssh-keygen -t rsa -b 4096 -f ~/.ssh/azure-oidc-issuer -N \"\"").
			Value(&oidcIssuerKeyPath).
			Validate(validateOIDCIssuerKeyPair),
	).WithHideFunc(func() bool {
		// AKS control planes are Azure-managed and keyless — nothing to ask.
		return cfg.AzureAKS
	})

	err := huh.NewForm(
		credGroup.Title("Azure credentials").Description("Step 3/4 (cont.)"),
		selfManagedGroup,
	).Run()
	if err != nil {
		return err
	}

	if cfg.AzureAKS {
		// Azure owns the control plane, and there is no storage account to
		// derive — nothing more to collect.
		return nil
	}

	cfg.AzureCPReplicas = "1"
	if haChoice {
		cfg.AzureCPReplicas = "3"
	}

	cfg.AzureOIDCIssuerKeyPath = expandTilde(oidcIssuerKeyPath)

	// Attempt to autofill the AAD service principal's object id via Microsoft
	// Graph (role assignments target it); fall back to a manual prompt —
	// reading service principals needs a Graph permission the app may lack.
	if cfg.AzurePrincipalID == "" {
		principalID, lookupErr := fetchAADServicePrincipalObjectID(
			context.Background(),
			http.DefaultClient,
			cfg.AzureTenantID, cfg.AzureClientID, cfg.AzureClientSecret,
		)
		if lookupErr != nil {
			slog.Warn("Failed resolving the AAD service principal object id via Microsoft Graph",
				slog.Any("error", lookupErr))
		}
		cfg.AzurePrincipalID = principalID
	}
	if cfg.AzurePrincipalID == "" {
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("AAD service principal object ID:").
					Description("az ad sp show --id <client-id> --query id -o tsv").
					Value(&cfg.AzurePrincipalID).
					Validate(nonEmpty),
			).Title("Azure AAD application").Description("Step 3/4 (cont.)"),
		).Run(); err != nil {
			return err
		}
	}

	// Auto-generate storage account name from cluster name.
	// Azure requires: 3-24 chars, lowercase alphanumeric only.
	var sb strings.Builder
	for _, r := range strings.ToLower(cfg.ClusterName) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	name := sb.String() + "sa"
	if len(name) > 24 {
		name = name[:24]
	}
	cfg.AzureStorageAccount = name

	return nil
}
