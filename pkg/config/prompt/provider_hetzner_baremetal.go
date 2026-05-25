// Copyright 2026 Obmondo
// SPDX-License-Identifier: AGPL3

package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
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
//
// The phases run as a tiny state machine so the operator can press
// shift+tab at the end-of-phase confirm to rewind one phase
// (e.g. realised at workers that the vSwitch subnet was wrong → back).
// huh doesn't surface "go back" across separate Run() calls
// natively; the post-phase confirm is the closest approximation.
func runHetznerBareMetalForm(cfg *PromptedConfig) error {
	sess := newBareMetalSession(cfg)

	cpCount, err := strconv.Atoi(cfg.HetznerCPReplicas)
	if err != nil || cpCount < 1 {
		return fmt.Errorf("hetzner bare-metal: invalid control-plane replica count %q", cfg.HetznerCPReplicas)
	}

	phase := bmPhaseVSwitch
	for phase != bmPhaseDone {
		next, err := runBareMetalPhase(cfg, sess, phase, cpCount)
		if err != nil {
			return err
		}
		phase = next
	}
	return nil
}

// bmPhase is one stage of the bare-metal flow. The state machine in
// runHetznerBareMetalForm walks these forward on Continue and
// rewinds them on shift+tab/Back at the phase-transition confirm.
type bmPhase int

const (
	defaultHetznerVSwitchSubnetCIDR = "10.0.1.0/24"

	bmPhaseVSwitch bmPhase = iota
	bmPhaseCP
	bmPhaseWorkers
	bmPhaseEndpoint
	bmPhaseDone
)

// runBareMetalPhase runs a single phase and returns the next bmPhase
// to enter. Split out from the for-loop so each case stays small
// and the "go back" jumps are obvious.
func runBareMetalPhase(cfg *PromptedConfig, sess *bareMetalSession, p bmPhase, cpCount int) (bmPhase, error) {
	switch p { //nolint:exhaustive // bmPhaseDone is the loop terminator; never passed in.
	case bmPhaseVSwitch:
		// kubeaid-cli's pure-BM path currently skips CreateVSwitch
		// (prerequisite_infrastructure.go:44 early-returns); the
		// config block is rendered so operators have a single
		// source of truth for their network plumbing, and so a
		// follow-up branch that lifts the early-return picks the
		// block up without YAML edits.
		if err := promptVSwitchConfig(cfg); err != nil {
			return bmPhaseDone, err
		}
		return bmPhaseCP, nil

	case bmPhaseCP:
		cfg.HetznerBMCPServerIDs = nil
		cfg.HetznerBMCPPrivateIPs = nil
		if err := addServerLoop(cfg, sess, roleControlPlane, cpCount); err != nil {
			return bmPhaseDone, err
		}
		if err := validateCPTopology(cfg); err != nil {
			return bmPhaseDone, err
		}
		goBack, err := promptPhaseTransition(
			fmt.Sprintf("Control plane configured — %d server(s) accepted", len(cfg.HetznerBMCPServerIDs)),
			"Continue to workers",
			"← Back to vSwitch")
		if err != nil {
			return bmPhaseDone, err
		}
		if goBack {
			return bmPhaseVSwitch, nil
		}
		return bmPhaseWorkers, nil

	case bmPhaseWorkers:
		cfg.HetznerBMNodeGroupServerIDs = nil
		cfg.HetznerBMNodeGroupPrivateIPs = nil
		if err := promptWorkerNodeGroupName(cfg); err != nil {
			return bmPhaseDone, err
		}
		if err := addServerLoop(cfg, sess, roleWorker, 0); err != nil {
			return bmPhaseDone, err
		}
		if err := validateWorkerTopology(cfg); err != nil {
			return bmPhaseDone, err
		}
		goBack, err := promptPhaseTransition(
			fmt.Sprintf("Workers configured — %d server(s) accepted", len(cfg.HetznerBMNodeGroupServerIDs)),
			"Continue to endpoint",
			"← Back to control plane")
		if err != nil {
			return bmPhaseDone, err
		}
		if goBack {
			return bmPhaseCP, nil
		}
		return bmPhaseEndpoint, nil

	case bmPhaseEndpoint:
		if err := promptBareMetalEndpoint(cfg); err != nil {
			return bmPhaseDone, err
		}
		return bmPhaseDone, nil
	}
	return bmPhaseDone, fmt.Errorf("hetzner bare-metal: unreachable phase %d", p)
}

// promptPhaseTransition shows a small Continue/Back confirm at the
// end of a phase. Plain huh default keymap — operator picks Continue
// or Go back with ←/→/y/n + Enter. Lets them rewind one phase to
// fix something they realised was wrong earlier (e.g. vSwitch subnet
// after collecting CPs).
//
// Returns (goBack=true, nil) when the operator picks the negative
// option; (goBack=false, nil) on Continue.
func promptPhaseTransition(title, continueLabel, backLabel string) (bool, error) {
	cont := true
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Affirmative(continueLabel).
				Negative(backLabel).
				Value(&cont),
		),
	).Run()
	if err != nil {
		return false, err
	}
	return !cont, nil
}

// runHetznerHybridBareMetalForm collects the bare-metal half of a
// hybrid cluster — HCloud control-plane sized by the existing
// HA / machineType form, plus a bare-metal worker node group that
// joins the HCloud Network via a kubeaid-cli-created vSwitch.
//
// Phases:
//
//	Phase 1 — vSwitch config (name / VLAN ID / subnet CIDR).
//	          Required: CreateVSwitch in
//	          pkg/cloud/hetzner/prerequisite_infrastructure.go runs
//	          unconditionally for hybrid mode and panics on a nil
//	          BareMetal.VSwitch.
//	Phase 2 — worker node-group name + add-loop. Each worker's
//	          private IP is validated against the vSwitch subnet
//	          from Phase 1 — the typical operator typo (IP outside
//	          the subnet) is caught before bootstrap.
//
// No CP / endpoint phases — those are HCloud (already collected by
// the parent credentials form).
func runHetznerHybridBareMetalForm(cfg *PromptedConfig) error {
	sess := newBareMetalSession(cfg)

	for {
		if err := promptVSwitchConfig(cfg); err != nil {
			return err
		}

		cfg.HetznerBMNodeGroupServerIDs = nil
		cfg.HetznerBMNodeGroupPrivateIPs = nil
		if err := promptWorkerNodeGroupName(cfg); err != nil {
			return err
		}
		if err := addServerLoop(cfg, sess, roleWorker, 0); err != nil {
			return err
		}
		if err := validateWorkerTopology(cfg); err != nil {
			return err
		}

		goBack, err := promptPhaseTransition(
			fmt.Sprintf("Workers configured — %d server(s) accepted", len(cfg.HetznerBMNodeGroupServerIDs)),
			"Continue — finish bare-metal setup",
			"← Back to vSwitch")
		if err != nil {
			return err
		}
		if !goBack {
			return nil
		}
		// Loop: shift+tab/Back → re-enter vSwitch + worker phase fresh.
	}
}

// bareMetalSession bundles the per-flow state every BM phase needs —
// the Robot lookup function, the sibling-config conflict map, and
// the (best-effort) pre-fetched list of Robot server IDs in the
// operator's account that powers autocomplete.
type bareMetalSession struct {
	lookup   robotServerLookup
	existing map[string]string
	// knownIDs is the operator's full Robot inventory, used as
	// huh.Input suggestions for tab-completion on the server-ID
	// field. nil when the pre-fetch failed (bad creds, Robot down,
	// or the override returned nil for tests) — the add-loop falls
	// back to typing-only.
	knownIDs []string
}

// newBareMetalSession builds the per-flow lookup function, sibling-
// config scan, and pre-fetched Robot server-ID list shared by the
// pure-BM and hybrid entry points. Pre-fetch runs under a spinner so
// the operator sees the wait; failures don't abort the flow (we just
// proceed without autocomplete) since the per-server lookup will
// surface the same auth/network errors later in a more localised
// way.
func newBareMetalSession(cfg *PromptedConfig) *bareMetalSession {
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

	if cfg.HetznerBMServerPublicIPs == nil {
		cfg.HetznerBMServerPublicIPs = make(map[string]string)
	}

	knownIDs := fetchKnownServerIDsWithSpinner(cfg)

	return &bareMetalSession{
		lookup:   lookup,
		existing: existing,
		knownIDs: knownIDs,
	}
}

// fetchKnownServerIDsWithSpinner returns the Robot inventory used to
// power the BM add-loop's autocomplete. The credential form's inline
// Validate(validateRobotCredentials) has already done the fetch under
// the operator's eyes — we just return the cached result. The
// spinner-backed live fetch remains as a fallback for the (rare)
// case where cfg.HetznerBMKnownServerIDs is empty (creds-only edit
// loop, or a future caller that bypasses the credential validator).
func fetchKnownServerIDsWithSpinner(cfg *PromptedConfig) []string {
	if len(cfg.HetznerBMKnownServerIDs) > 0 {
		return cfg.HetznerBMKnownServerIDs
	}

	var (
		ids []string
		err error
	)
	_ = spinner.New().
		Title("  Fetching your Hetzner Robot server list ...").
		Action(func() {
			ids, err = robotListLookup(cfg.HetznerRobotUser, cfg.HetznerRobotPassword)
		}).
		Run()
	if err != nil {
		// Per-server lookup will produce a clearer, scoped error if
		// the same auth/network issue persists. Silently degrade.
		return nil
	}
	cfg.HetznerBMKnownServerIDs = ids
	return ids
}

// robotListLookup is the test-overridable indirection around
// robotClientList — kept narrow so the credential-form validator can
// invoke the same fetch path without depending on a built client.
func robotListLookup(user, password string) ([]string, error) {
	if robotListOverride != nil {
		return robotListOverride()
	}
	return robotClientList(newRobotClient(user, password))
}

// validateRobotCredentials returns a huh validator that uses the
// just-typed password (and the username already captured in cfg) to
// hit Robot's GET /server. nil on success — and the resulting
// inventory is stashed on cfg for downstream autocomplete; an error
// otherwise, displayed inline by huh so the operator can fix the
// password (or Shift+Tab back to the username) without re-running
// the whole Hetzner credentials form.
//
// nonEmpty short-circuits before the API call so an Enter on a blank
// password doesn't trigger a guaranteed 401 — same shape as the
// other Validate-fn closures in this package.
func validateRobotCredentials(cfg *PromptedConfig) func(string) error {
	return func(s string) error {
		if err := nonEmpty(s); err != nil {
			return err
		}
		if cfg.HetznerRobotUser == "" {
			// Defensive — huh should have validated this on the
			// previous field, but if the operator force-tabbed
			// through, fall back to a clear message instead of
			// triggering a 401 we'd have to translate.
			return errors.New("robot username must be set before validating password")
		}
		ids, err := robotListLookup(cfg.HetznerRobotUser, s)
		if err != nil {
			return err
		}
		cfg.HetznerBMKnownServerIDs = ids
		return nil
	}
}

// promptVSwitchConfig collects the vSwitch resource details kubeaid-cli
// passes to CreateVSwitch. Defaults are sensible-but-overridable —
// the operator typically wants the cluster-name-prefixed defaults
// unless they're joining an existing vSwitch.
func promptVSwitchConfig(cfg *PromptedConfig) error {
	if cfg.HetznerVSwitchName == "" {
		cfg.HetznerVSwitchName = cfg.ClusterName + "-vswitch"
	}
	if cfg.HetznerVSwitchVLANID == "" {
		cfg.HetznerVSwitchVLANID = "4000"
	}
	if cfg.HetznerVSwitchSubnetCIDR == "" {
		cfg.HetznerVSwitchSubnetCIDR = defaultHetznerVSwitchSubnetCIDR
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("vSwitch name:").
				Description("Hetzner Robot identifier — kubeaid-cli creates the vSwitch if not already present, or reuses one matching this name + VLAN ID.").
				Value(&cfg.HetznerVSwitchName).
				Validate(nonEmpty),
			huh.NewInput().
				Title("vSwitch VLAN ID (4000-4091):").
				Value(&cfg.HetznerVSwitchVLANID).
				Validate(hetznerVLANID),
			huh.NewInput().
				Title("vSwitch subnet CIDR (worker private IPs must live here):").
				Value(&cfg.HetznerVSwitchSubnetCIDR).
				Validate(cidrv4),
		).Title("Hetzner Bare Metal — vSwitch network"),
	).Run()
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
// private IP, an inline Robot probe, a result note, then either an
// "add another?" prompt (open-ended worker mode) or an automatic
// advance to the next slot (fixed-count CP mode).
//
// fixedCount == 0 → worker mode: loop ends when operator answers "no
// more".
// fixedCount  > 0 → CP mode: loop ends when fixedCount servers have
// been added. The HA toggle pins CP at 1 or 3 — even counts lose
// etcd quorum, so we don't surface them in the prompt.
func addServerLoop(
	cfg *PromptedConfig,
	sess *bareMetalSession,
	r role,
	fixedCount int,
) error {
	for {
		var (
			serverID = ""
			// Pre-fill privateIP with the next free slot in the
			// vSwitch subnet. Operator presses enter to accept or
			// types over the suggestion — the typical case is a
			// simple sequential layout (.1 .2 .3 for CPs, .10+ for
			// workers).
			privateIP = nextPrivateIPInSubnet(cfg.HetznerVSwitchSubnetCIDR, currentlyUsedIPs(cfg, r))
		)

		// When the operator has supplied a vSwitch subnet (hybrid
		// mode today), the per-host IP must live inside it — catches
		// the common typo before bootstrap's network setup fails.
		// Pure-BM mode skips the containment check (cidr is empty).
		ipValidator := ipv4InSubnet(cfg.HetznerVSwitchSubnetCIDR)

		idInput := huh.NewInput().
			TitleFunc(func() string {
				return fmt.Sprintf("%s server #%d — Robot server ID:", capitalize(r.label()), nextIndex(cfg, r))
			}, cfg).
			Value(&serverID).
			Validate(validateBMServerID(cfg))
		if len(sess.knownIDs) > 0 {
			// huh binds Tab to "next field" and Ctrl+E to "accept
			// suggestion" by default; calling that out in the
			// description keeps the operator from getting stuck
			// looking for the autocomplete key.
			idInput = idInput.
				Description("Ctrl+E autocompletes from your Robot inventory; Enter/Tab advances.").
				Suggestions(sess.knownIDs)
		}

		if err := huh.NewForm(
			huh.NewGroup(
				idInput,
				huh.NewInput().
					Title("Private IPv4 (vSwitch):").
					Description("Pre-filled with the next free address in the vSwitch subnet — edit if you have a specific assignment.").
					Value(&privateIP).
					Validate(ipValidator),
			).Title(fmt.Sprintf("Hetzner Bare Metal — %s server", r.label())),
		).Run(); err != nil {
			return err
		}

		serverID = strings.TrimSpace(serverID)
		privateIP = strings.TrimSpace(privateIP)

		info, lookupErr := sess.lookup(serverID)
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
		conflictCluster := sess.existing[serverID]

		keep, err := confirmAcceptedServer(serverID, info, conflictCluster, r)
		if err != nil {
			return err
		}
		if !keep {
			continue
		}

		appendServer(cfg, r, serverID, privateIP, info)

		if fixedCount > 0 {
			// CP mode: HA toggle has pinned the count. Loop straight
			// into the next slot without prompting — operator's
			// already committed to 1 or 3.
			if nextIndex(cfg, r) > fixedCount {
				return nil
			}
			continue
		}

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
// in the summary box + YAML comments. For control-plane servers, also
// stores the server's Hetzner region (derived from Robot's DC field)
// onto cfg.HetznerBMCPRegions — without this the rendered chart values
// carry `regions: []` and ArgoCD's CAPH schema check rejects the sync.
func appendServer(cfg *PromptedConfig, r role, id, privateIP string, info *robotServerInfo) {
	switch r {
	case roleControlPlane:
		cfg.HetznerBMCPServerIDs = append(cfg.HetznerBMCPServerIDs, id)
		cfg.HetznerBMCPPrivateIPs = append(cfg.HetznerBMCPPrivateIPs, privateIP)
		if info != nil {
			cfg.HetznerBMCPRegions = appendUniqueRegion(cfg.HetznerBMCPRegions, hetznerDCToRegion(info.DC))
		}
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

// hetznerDCToRegion maps a Hetzner Robot DC name (e.g. "FSN1-DC14",
// "HEL1-DC2", "ASH-DC1") to the lower-case region identifier that
// CAPH's chart accepts ("fsn1", "hel1", "ash"). Robot returns the DC
// with a `-DC<rack>` suffix; HCloud regions don't carry that suffix,
// so we trim it.
//
// Case-insensitive on the separator so test fixtures using lowercase
// `-dc<n>` form behave the same as the upstream uppercase. Returns ""
// on empty input so callers can skip adding it to the region set
// rather than push an empty string and trip schema `pattern` checks
// downstream.
func hetznerDCToRegion(dc string) string {
	if dc == "" {
		return ""
	}
	upper := strings.ToUpper(dc)
	base, _, _ := strings.Cut(upper, "-DC")
	return strings.ToLower(base)
}

// appendUniqueRegion adds region to regions if it isn't already
// present and non-empty. Preserves insertion order so the rendered
// regions list reflects the order servers were added — keeps re-runs
// of the prompt against the same server set deterministic, which
// matters for SealedSecret byte-stability and the no-op-diff guard
// in AddCommitAndPushChanges.
func appendUniqueRegion(regions []string, region string) []string {
	if region == "" {
		return regions
	}
	for _, existing := range regions {
		if existing == region {
			return regions
		}
	}
	return append(regions, region)
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
//
// Default is just "workers" rather than "<cluster-name>-workers":
// the cluster name already lives in cluster.name and shows up in
// every label / annotation that needs cluster context; baking it
// into the node-group label again only adds noise (operators see
// "kbm-obmondo-com / kbm-obmondo-com-workers" in the storage-plan
// tree, kubectl get nodes, etc.).
func promptWorkerNodeGroupName(cfg *PromptedConfig) error {
	if cfg.HetznerBMNodeGroupName == "" {
		cfg.HetznerBMNodeGroupName = "workers"
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

// validateBMServerID is the huh.Input validator the add-loop wires
// onto the Robot server-ID field. Runs the format check first
// (numeric, non-blank — its error message points at the obvious
// fix) and then rejects IDs already added elsewhere in the
// bare-metal flow. kubeaid-cli's bootstrap can't provision the
// same Robot server twice, and catching it inline puts the error
// right under the field where the operator typed it.
func validateBMServerID(cfg *PromptedConfig) func(string) error {
	return func(s string) error {
		if err := serverIDValidator(s); err != nil {
			return err
		}
		role := lookupExistingBMRole(cfg, s)
		if role == "" {
			return nil
		}
		return fmt.Errorf("server %s is already added as a %s in this cluster",
			strings.TrimSpace(s), role)
	}
}

// lookupExistingBMRole returns a human-readable label describing
// where id already appears in cfg's bare-metal slices, or "" if
// it's not yet been added. Used by the add-loop's inline Validate
// so an operator pasting the same Robot server ID twice (e.g.
// fat-fingering Ctrl+V) gets an error right under the field.
func lookupExistingBMRole(cfg *PromptedConfig, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, existing := range cfg.HetznerBMCPServerIDs {
		if existing == id {
			return "control-plane host"
		}
	}
	for _, existing := range cfg.HetznerBMNodeGroupServerIDs {
		if existing == id {
			return "worker host"
		}
	}
	return ""
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

// robotLookupOverride and robotListOverride are unset in production;
// tests override them to short-circuit the Robot client construction.
var (
	robotLookupOverride robotServerLookup
	robotListOverride   func() ([]string, error)
)

// robotListResponse is the wire shape of Hetzner Robot's GET /server
// — an array of {server: {...}} objects (note the outer array, not
// the wrapped-in-server shape GET /server/<id> returns).
type robotListEntry struct {
	Server struct {
		ServerNumber int    `json:"server_number"`
		ServerName   string `json:"server_name"`
		Status       string `json:"status"`
		Cancelled    bool   `json:"cancelled"`
	} `json:"server"`
}

// robotClientList performs GET /server against Robot and returns the
// IDs of every server in the account that's healthy enough to be a
// candidate cluster member — cancelled servers and non-ready states
// are filtered so the autocomplete list doesn't tempt the operator
// toward a box that won't bootstrap.
func robotClientList(client *resty.Client) ([]string, error) {
	resp, err := client.R().Get("/server")
	if err != nil {
		return nil, fmt.Errorf("network error contacting Robot webservice: %w", err)
	}
	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, errors.New("robot username/password rejected (401)")
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("unexpected Robot status %d", resp.StatusCode())
	}

	var entries []robotListEntry
	if err := json.Unmarshal(resp.Body(), &entries); err != nil {
		return nil, fmt.Errorf("decoding Robot list response: %w", err)
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Server.Cancelled {
			continue
		}
		if e.Server.Status != "" && e.Server.Status != "ready" {
			continue
		}
		ids = append(ids, strconv.Itoa(e.Server.ServerNumber))
	}
	return ids, nil
}

// nextPrivateIPInSubnet returns the first IPv4 in cidr (skipping the
// network and broadcast addresses) that's not already in used. Used
// to pre-fill the per-host privateIP field — operator presses enter
// to accept the suggestion or types over it. Returns "" when cidr is
// empty or unparseable (pure-BM without vSwitch, or malformed input).
func nextPrivateIPInSubnet(cidr string, used []string) string {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return ""
	}
	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil || subnet == nil {
		return ""
	}
	v4 := subnet.IP.To4()
	if v4 == nil {
		return ""
	}

	usedSet := make(map[string]struct{}, len(used))
	for _, ip := range used {
		usedSet[strings.TrimSpace(ip)] = struct{}{}
	}

	// Walk IPv4 addresses by incrementing the last octet first, then
	// rippling up. Caps at 65536 iterations so a /16 with everything
	// taken still terminates — that's well beyond any realistic
	// cluster size, and infinite-looping on a bad input would be
	// worse than returning an empty default.
	cand := make(net.IP, len(v4))
	copy(cand, v4)
	for range 65536 {
		incrementIPv4(cand)
		if !subnet.Contains(cand) {
			return ""
		}
		s := cand.String()
		// Skip the network's broadcast tail (.255 on a /24, etc.).
		if isBroadcast(cand, subnet) {
			continue
		}
		if _, taken := usedSet[s]; taken {
			continue
		}
		return s
	}
	return ""
}

func incrementIPv4(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] != 0 {
			return
		}
	}
}

func isBroadcast(ip net.IP, subnet *net.IPNet) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	mask := subnet.Mask
	if len(mask) != net.IPv4len {
		return false
	}
	for i := range v4 {
		if v4[i]|mask[i] != 0xff {
			return false
		}
	}
	return true
}

// currentlyUsedIPs returns the privateIPs the operator has already
// accepted in this BM flow — CP IPs across both roles when adding a
// worker, both when adding another CP. Used to seed the next-free-IP
// suggestion so the same address never gets proposed twice.
func currentlyUsedIPs(cfg *PromptedConfig, _ role) []string {
	out := make([]string, 0, len(cfg.HetznerBMCPPrivateIPs)+len(cfg.HetznerBMNodeGroupPrivateIPs))
	out = append(out, cfg.HetznerBMCPPrivateIPs...)
	out = append(out, cfg.HetznerBMNodeGroupPrivateIPs...)
	return out
}

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
