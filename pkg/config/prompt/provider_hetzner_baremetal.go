// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
)

// runHetznerBareMetalForm collects bare-metal config in three phases:
//
//	Phase 1 — add CP servers one at a time, Robot-validate inline, loop until
//	          the operator chooses "no more".
//	Phase 2 — pick a worker node-group name, then add worker servers the same
//	          way.
//	Phase 3 — pick the kube-apiserver endpoint (Failover IP or a CP node's
//	          main IP) and confirm the host.
//
// Per-server Robot lookup happens inline (rather than batched at the end)
// so a typo on the first server is surfaced before the operator types five
// more — same intent as the rest of the prompt's fail-fast loops.
//
// Pre-condition: cfg.HetznerRobotUser and cfg.HetznerRobotPassword are
// already populated from the parent Hetzner credentials form.
func runHetznerBareMetalForm(cfg *PromptedConfig) error {
	lookup := robotLookupOverride
	if lookup == nil {
		client := newRobotClient(cfg.HetznerRobotUser, cfg.HetznerRobotPassword)
		lookup = func(id string) (*robotServerInfo, error) { return robotClientLookup(client, id) }
	}

	// Sibling-config scan: free signal — if the operator already gave
	// this server ID to another cluster generated under the same
	// outputs/configs/ tree, surface a non-blocking warning so they
	// notice the conflict before bootstrap blows up at provisioning
	// time. Skipped silently when ConfigsDirectory is unset (e.g.
	// tests that drive the prompt without a real configs dir).
	existing := scanSiblingConfigsForServerIDs(cfg.ConfigsDirectory)

	// Reset slices to drop any values from a previous loop iteration
	// of ConfigFromPrompt — fresh runs of an edit loop should start
	// the add-loop from scratch.
	cfg.HetznerBMCPServerIDs = nil
	cfg.HetznerBMCPPrivateIPs = nil
	cfg.HetznerBMNodeGroupServerIDs = nil
	cfg.HetznerBMNodeGroupPrivateIPs = nil
	if cfg.HetznerBMServerPublicIPs == nil {
		cfg.HetznerBMServerPublicIPs = make(map[string]string)
	}

	if err := addServerLoop(cfg, lookup, existing, roleControlPlane); err != nil {
		return err
	}
	if err := validateCPTopology(cfg); err != nil {
		return err
	}

	if err := promptWorkerNodeGroupName(cfg); err != nil {
		return err
	}
	if err := addServerLoop(cfg, lookup, existing, roleWorker); err != nil {
		return err
	}
	if err := validateWorkerTopology(cfg); err != nil {
		return err
	}

	if err := promptBareMetalEndpoint(cfg); err != nil {
		return err
	}

	// Reflect the actual CP count into HetznerCPReplicas so the
	// summary box and any downstream readers see the same number
	// the YAML will carry.
	cfg.HetznerCPReplicas = strconv.Itoa(len(cfg.HetznerBMCPServerIDs))

	return nil
}

// role describes which phase of the add-loop is running. Affects
// form titles, where the collected server is appended on cfg, and
// which topology check applies at the end.
type role int

const (
	roleControlPlane role = iota
	roleWorker
)

func (r role) label() string {
	switch r {
	case roleControlPlane:
		return "control-plane"
	case roleWorker:
		return "worker"
	}
	return "worker"
}

// addServerLoop runs the per-role add-loop: one form to collect ID +
// private IP, an inline Robot probe, a result note, then an "add
// another?" prompt. Exits when the operator answers No.
func addServerLoop(
	cfg *PromptedConfig,
	lookup robotServerLookup,
	existingServers map[string]string,
	r role,
) error {
	for {
		var (
			serverID  string
			privateIP string
		)

		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					TitleFunc(func() string {
						return fmt.Sprintf("%s server #%d — Robot server ID:", capitalize(r.label()), nextIndex(cfg, r))
					}, cfg).
					Value(&serverID).
					Validate(serverIDValidator),
				huh.NewInput().
					Title("Private IPv4 (vSwitch):").
					Value(&privateIP).
					Validate(ipv4),
			).Title(fmt.Sprintf("Hetzner Bare Metal — %s server", r.label())),
		).Run(); err != nil {
			return err
		}

		serverID = strings.TrimSpace(serverID)
		privateIP = strings.TrimSpace(privateIP)

		info, lookupErr := lookup(serverID)
		if lookupErr != nil {
			action, err := promptLookupFailureAction(serverID, lookupErr)
			if err != nil {
				return err
			}
			switch action {
			case lookupRetry:
				continue
			case lookupAccept:
				// Operator forced acceptance of an unreachable server
				// — not actually offered by the failure prompt today,
				// but allowed for future extension.
			case lookupCancel:
				return fmt.Errorf("hetzner bare-metal: %w", lookupErr)
			}
		}

		// Robot status / cancellation soft-blocks.
		if action, err := promptStatusGuard(serverID, info); err != nil {
			return err
		} else if action == lookupRetry {
			continue
		}

		// Sibling-config warning — informational only; operator may be
		// deliberately re-using the box (re-bootstrap, recovery).
		conflictCluster := existingServers[serverID]

		keep, err := confirmAcceptedServer(serverID, info, conflictCluster, r)
		if err != nil {
			return err
		}
		if !keep {
			continue
		}

		appendServer(cfg, r, serverID, privateIP, info)

		more, err := promptAddAnother(cfg, r)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// nextIndex returns the 1-based ordinal of the *next* server the
// add-loop will collect, used for the form title ("control-plane
// server #2 — Robot server ID:").
func nextIndex(cfg *PromptedConfig, r role) int {
	switch r {
	case roleControlPlane:
		return len(cfg.HetznerBMCPServerIDs) + 1
	case roleWorker:
		return len(cfg.HetznerBMNodeGroupServerIDs) + 1
	}
	return 0
}

// appendServer records an accepted server against the appropriate
// role's slice on cfg, and decorates the public-IP map for later use
// in the summary box + YAML comments.
func appendServer(cfg *PromptedConfig, r role, id, privateIP string, info *robotServerInfo) {
	switch r {
	case roleControlPlane:
		cfg.HetznerBMCPServerIDs = append(cfg.HetznerBMCPServerIDs, id)
		cfg.HetznerBMCPPrivateIPs = append(cfg.HetznerBMCPPrivateIPs, privateIP)
	case roleWorker:
		cfg.HetznerBMNodeGroupServerIDs = append(cfg.HetznerBMNodeGroupServerIDs, id)
		cfg.HetznerBMNodeGroupPrivateIPs = append(cfg.HetznerBMNodeGroupPrivateIPs, privateIP)
	default:
		// Unreachable — role values are enumerated by this package.
	}
	if info != nil {
		cfg.HetznerBMServerPublicIPs[id] = info.PublicIP
	}
}

// lookupAction is the operator's choice after a Robot lookup outcome —
// retry the same server (re-runs the input form pre-filled with the
// last attempt's values) or cancel the whole bare-metal flow.
type lookupAction int

const (
	lookupRetry lookupAction = iota
	lookupCancel
	lookupAccept
)

func promptLookupFailureAction(id string, lookupErr error) (lookupAction, error) {
	retry := true
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(fmt.Sprintf("Robot rejected server %s", id)).
				Description(lookupErr.Error()),
			huh.NewConfirm().
				Title("What now?").
				Affirmative("Edit and retry").
				Negative("Cancel bare-metal setup").
				Value(&retry),
		),
	).Run()
	if err != nil {
		return lookupCancel, err
	}
	if retry {
		return lookupRetry, nil
	}
	return lookupCancel, nil
}

// promptStatusGuard surfaces Robot states that almost-always indicate
// "don't use this box" — cancelled or not-yet-ready servers. The
// operator can still override (sometimes they're picking a box that's
// just been re-ordered and Robot hasn't flipped to ready yet).
func promptStatusGuard(id string, info *robotServerInfo) (lookupAction, error) {
	if info == nil {
		return lookupAccept, nil
	}

	var reason string
	switch {
	case info.Cancelled:
		reason = fmt.Sprintf("Server %s is marked cancelled in Robot (paid_until=%s).", id, info.PaidUntil)
	case info.Status != "" && info.Status != "ready":
		reason = fmt.Sprintf("Server %s is in Robot state %q (not 'ready').", id, info.Status)
	default:
		return lookupAccept, nil
	}

	useAnyway := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Heads-up — Robot status check").
				Description(reason+"\nUse it anyway only if you know this is the right box (e.g. it just rebooted)."),
			huh.NewConfirm().
				Title("Use this server?").
				Affirmative("No, pick a different one").
				Negative("Yes, use it anyway").
				Value(&useAnyway),
		),
	).Run()
	if err != nil {
		return lookupCancel, err
	}
	if useAnyway {
		return lookupRetry, nil
	}
	return lookupAccept, nil
}

func confirmAcceptedServer(
	id string,
	info *robotServerInfo,
	conflictCluster string,
	r role,
) (bool, error) {
	desc := renderServerInfo(info)
	if conflictCluster != "" {
		desc += fmt.Sprintf(
			"\n\n⚠  Server %s is also listed under %s/general.yaml — using it here will collide if that cluster is still alive.",
			id, conflictCluster,
		)
	}

	keep := true
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(fmt.Sprintf("Server %s confirmed by Robot", id)).
				Description(desc),
			huh.NewConfirm().
				Title(fmt.Sprintf("Add this server to the %s set?", r.label())).
				Affirmative("Yes, add it").
				Negative("No, retype").
				Value(&keep),
		),
	).Run()
	return keep, err
}

func promptAddAnother(cfg *PromptedConfig, r role) (bool, error) {
	var addAnother bool
	count := len(cfg.HetznerBMCPServerIDs)
	if r == roleWorker {
		count = len(cfg.HetznerBMNodeGroupServerIDs)
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Add another %s server?", r.label())).
				Description(fmt.Sprintf("So far: %d %s server(s).", count, r.label())).
				Affirmative(fmt.Sprintf("Yes, add %s #%d", r.label(), count+1)).
				Negative(fmt.Sprintf("No, that's all (%d %s)", count, r.label())).
				Value(&addAnother),
		),
	).Run()
	return addAnother, err
}

// renderServerInfo lays out the inline ✓-confirmation note shown to
// the operator after a successful Robot lookup. Kept terse — one
// line, four facts: friendly name, hardware, datacenter, main IP.
func renderServerInfo(info *robotServerInfo) string {
	if info == nil {
		return "(Robot returned no metadata)"
	}
	parts := []string{}
	if info.Name != "" {
		parts = append(parts, info.Name)
	}
	if info.Product != "" {
		parts = append(parts, info.Product)
	}
	if info.DC != "" {
		parts = append(parts, info.DC)
	}
	if info.PublicIP != "" {
		parts = append(parts, "main IP "+info.PublicIP)
	}
	if info.PaidUntil != "" {
		parts = append(parts, "paid until "+info.PaidUntil)
	}
	if len(parts) == 0 {
		return "(Robot returned no usable metadata)"
	}
	return "✓ " + strings.Join(parts, " — ")
}

// promptWorkerNodeGroupName collects the single worker node-group
// name. One group only at prompt time — multi-group setups are
// supported by editing general.yaml after generation.
func promptWorkerNodeGroupName(cfg *PromptedConfig) error {
	if cfg.HetznerBMNodeGroupName == "" {
		cfg.HetznerBMNodeGroupName = cfg.ClusterName + "-workers"
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Worker node-group name:").
				Description("Multi-group setups: edit general.yaml after generation to add more groups.").
				Value(&cfg.HetznerBMNodeGroupName).
				Validate(nonEmpty),
		),
	).Run()
}

// promptBareMetalEndpoint asks Failover-IP-or-not and the endpoint
// host. When the operator chooses "not Failover", the host defaults
// to the first CP node's main IP — saves a paste and is usually
// correct for single-CP test clusters.
func promptBareMetalEndpoint(cfg *PromptedConfig) error {
	if !cfg.HetznerBMEndpointIsFailoverIP && cfg.HetznerBMEndpointHost == "" && len(cfg.HetznerBMCPServerIDs) > 0 {
		if ip := cfg.HetznerBMServerPublicIPs[cfg.HetznerBMCPServerIDs[0]]; ip != "" {
			cfg.HetznerBMEndpointHost = ip
		}
	}
	// Default to Failover when none of the above gave us a sensible
	// host — the production-shape choice.
	if cfg.HetznerBMEndpointHost == "" {
		cfg.HetznerBMEndpointIsFailoverIP = true
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Is the kube-apiserver endpoint a Hetzner Failover IP?").
				Description("Recommended for HA — kubeaid-cli switches it to the active control-plane node during bootstrap.").
				Affirmative("Yes — Failover IP").
				Negative("No — use a CP node's main IP").
				Value(&cfg.HetznerBMEndpointIsFailoverIP),
			huh.NewInput().
				Title("kube-apiserver endpoint IPv4:").
				Value(&cfg.HetznerBMEndpointHost).
				Validate(ipv4),
		).Title("Hetzner Bare Metal — API endpoint"),
	).Run()
}

// validateCPTopology guards the two cases that produce a broken
// cluster: zero CP nodes (no quorum source), and even CP counts
// (etcd quorum 50/50, no HA win). The "zero CPs" case shouldn't be
// reachable through the form (operator must answer "yes, add one"
// at least once), but we belt-and-suspenders for completeness.
func validateCPTopology(cfg *PromptedConfig) error {
	n := len(cfg.HetznerBMCPServerIDs)
	if n == 0 {
		return errors.New("hetzner bare-metal: at least one control-plane server is required")
	}
	if n%2 == 0 {
		return fmt.Errorf("hetzner bare-metal: control-plane count must be odd for etcd quorum (got %d)", n)
	}
	return nil
}

func validateWorkerTopology(cfg *PromptedConfig) error {
	if len(cfg.HetznerBMNodeGroupServerIDs) == 0 {
		return errors.New("hetzner bare-metal: at least one worker server is required")
	}
	return nil
}

// scanSiblingConfigsForServerIDs walks the parent of configsDirectory
// looking for other clusters' general.yaml, and builds a map of
// serverID → cluster-name. Used to surface "you already gave this
// server to cluster X" warnings at prompt time. Best-effort: errors
// in scanning (missing dir, malformed YAML) are silenced — the
// resulting empty map just disables the check.
func scanSiblingConfigsForServerIDs(configsDirectory string) map[string]string {
	if configsDirectory == "" {
		return nil
	}
	parent := filepath.Dir(filepath.Clean(configsDirectory))
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}

	out := map[string]string{}
	self := filepath.Base(filepath.Clean(configsDirectory))

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == self {
			continue
		}
		generalPath := filepath.Join(parent, entry.Name(), "general.yaml")
		body, err := os.ReadFile(generalPath)
		if err != nil {
			continue
		}
		for _, id := range extractServerIDs(body) {
			out[id] = entry.Name()
		}
	}
	return out
}

// extractServerIDs walks a general.yaml's YAML tree for serverID
// values under the Hetzner bareMetal blocks. Tolerant of partial /
// malformed files — returns whatever it finds.
func extractServerIDs(body []byte) []string {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil
	}
	var ids []string
	walkYAMLForServerIDs(&root, &ids)
	return ids
}

func walkYAMLForServerIDs(n *yaml.Node, ids *[]string) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			if key.Value == "serverID" && val.Kind == yaml.ScalarNode {
				v := strings.TrimSpace(val.Value)
				if v != "" {
					*ids = append(*ids, v)
				}
				continue
			}
			walkYAMLForServerIDs(val, ids)
		}
		return
	}
	for _, c := range n.Content {
		walkYAMLForServerIDs(c, ids)
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// serverIDValidator accepts a non-empty numeric Robot server ID.
// Robot IDs are always integers; rejecting at prompt time avoids a
// guaranteed 400 from the Robot webservice and a cryptic error
// message.
func serverIDValidator(s string) error {
	if err := nonEmpty(s); err != nil {
		return err
	}
	if _, err := strconv.Atoi(strings.TrimSpace(s)); err != nil {
		return errors.New("server ID must be numeric (e.g. 1234567)")
	}
	return nil
}

// robotServerLookup is the surface the add-loop uses to talk to the
// Robot webservice — split out so unit tests can inject a fake without
// standing up an HTTP server.
type robotServerLookup func(id string) (*robotServerInfo, error)

// robotLookupOverride is unset in production; tests override it to
// short-circuit the Robot client construction.
var robotLookupOverride robotServerLookup

// robotServerInfo is the slice of GET /server/<id> the prompt
// surfaces back to the operator — enough to confirm the picked box
// is the right one, no more.
type robotServerInfo struct {
	ID        string
	PublicIP  string
	Name      string
	Product   string
	DC        string
	Status    string
	Cancelled bool
	PaidUntil string
}

// newRobotClient mirrors NewHetznerCloudProvider's resty setup
// (basic-auth + form-encoded + JSON accept) so this prompt-time
// probe sees the same response shape the bootstrap will later.
func newRobotClient(user, password string) *resty.Client {
	return resty.New().
		SetBaseURL(constants.HetznerRobotWebServiceAPI).
		SetBasicAuth(user, password).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetHeader("Accept", "application/json").
		SetTimeout(15 * time.Second)
}

// robotServerResponse holds the subset of GET /server/<id> we care
// about. The endpoint returns more (rescue / vnc flags, traffic
// allotment, subnets) but they're not load-bearing for the prompt.
type robotServerResponse struct {
	Server struct {
		ServerIP     string `json:"server_ip"`
		ServerNumber int    `json:"server_number"`
		ServerName   string `json:"server_name"`
		Product      string `json:"product"`
		DC           string `json:"dc"`
		Status       string `json:"status"`
		Cancelled    bool   `json:"cancelled"`
		PaidUntil    string `json:"paid_until"`
	} `json:"server"`
}

// robotClientLookup performs one GET /server/<id> against Robot and
// hydrates a robotServerInfo. Errors carry just enough context for
// the operator: bad creds, no such ID, network down.
func robotClientLookup(client *resty.Client, id string) (*robotServerInfo, error) {
	id = strings.TrimSpace(id)
	resp, err := client.R().Get("/server/" + id)
	if err != nil {
		return nil, fmt.Errorf("network error contacting Robot webservice: %w", err)
	}

	switch resp.StatusCode() {
	case http.StatusOK:
		// fall through
	case http.StatusUnauthorized:
		return nil, errors.New("robot username/password rejected (401) — re-enter them")
	case http.StatusNotFound:
		return nil, errors.New("no such server in this Robot account (404)")
	default:
		return nil, fmt.Errorf("unexpected Robot status %d", resp.StatusCode())
	}

	var body robotServerResponse
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		return nil, fmt.Errorf("decoding Robot response: %w", err)
	}
	if body.Server.ServerIP == "" {
		return nil, errors.New("robot returned no main IP for this server")
	}
	return &robotServerInfo{
		ID:        id,
		PublicIP:  body.Server.ServerIP,
		Name:      body.Server.ServerName,
		Product:   body.Server.Product,
		DC:        body.Server.DC,
		Status:    body.Server.Status,
		Cancelled: body.Server.Cancelled,
		PaidUntil: body.Server.PaidUntil,
	}, nil
}
