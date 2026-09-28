// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Obmondo/kubeaid-cli/pkg/constants"
)

// RenderFunc renders the security operations files into a cluster
// directory and returns the written paths, relative to it. The commands pass
// core.RenderSecurityOperations, so every change to the render (output
// locations, credential rotation) flows into apply, upgrade and the tests.
type RenderFunc func(ctx context.Context, clusterDir string) ([]string, error)

// SyncOrderLabel is the label the rendered Applications carry their sync
// order in.
const SyncOrderLabel = "kubeaid.io/sync-order"

// kindApplication is Argo CD's Application kind.
const kindApplication = "Application"

// gitStatus is the git subcommand Dirty runs.
const gitStatus = "status"

// Application is the part of a rendered Argo CD Application siemctl needs.
type Application struct {
	Name      string
	Namespace string
	Order     int
	// File is where it was rendered, relative to the cluster directory.
	File string
	// Sources are the spec.sources (or spec.source) entries.
	Sources []ApplicationSource
}

// ApplicationSource is one Argo CD Application source.
type ApplicationSource struct {
	RepoURL        string
	Path           string
	TargetRevision string
	Ref            string
	ValueFiles     []string
	ValuesObject   map[string]any
	// Directory is set on a plain directory source (the sealed Secrets the
	// Application owns), which carries a path but no chart.
	Directory bool
}

// ReadApplications parses every Argo CD Application in the given files
// (relative to clusterDir). Files that are not YAML Applications are skipped.
func ReadApplications(clusterDir string, files []string) ([]Application, error) {
	var apps []Application
	for _, rel := range files {
		if !strings.HasSuffix(rel, ".yaml") && !strings.HasSuffix(rel, ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(clusterDir, rel))
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name   string            `yaml:"name"`
					Labels map[string]string `yaml:"labels"`
				} `yaml:"metadata"`
				Spec struct {
					Destination struct {
						Namespace string `yaml:"namespace"`
					} `yaml:"destination"`
					Source  *rawSource  `yaml:"source"`
					Sources []rawSource `yaml:"sources"`
				} `yaml:"spec"`
			}
			if err := dec.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				// Not a plain YAML file (e.g. a Helm template); skip it.
				break
			}
			if doc.Kind != kindApplication || doc.Metadata.Name == "" {
				continue
			}
			order, _ := strconv.Atoi(doc.Metadata.Labels[SyncOrderLabel])
			app := Application{
				Name:      doc.Metadata.Name,
				Namespace: doc.Spec.Destination.Namespace,
				Order:     order,
				File:      rel,
			}
			sources := doc.Spec.Sources
			if doc.Spec.Source != nil {
				sources = append([]rawSource{*doc.Spec.Source}, sources...)
			}
			for _, s := range sources {
				app.Sources = append(app.Sources, ApplicationSource{
					RepoURL:        s.RepoURL,
					Path:           s.Path,
					TargetRevision: s.TargetRevision,
					Ref:            s.Ref,
					ValueFiles:     s.Helm.ValueFiles,
					ValuesObject:   s.Helm.ValuesObject,
					Directory:      s.Directory != nil,
				})
			}
			apps = append(apps, app)
		}
	}
	return apps, nil
}

type rawSource struct {
	RepoURL        string `yaml:"repoURL"`
	Path           string `yaml:"path"`
	TargetRevision string `yaml:"targetRevision"`
	Ref            string `yaml:"ref"`
	Helm           struct {
		ValueFiles   []string       `yaml:"valueFiles"`
		ValuesObject map[string]any `yaml:"valuesObject"`
	} `yaml:"helm"`
	Directory *struct {
		Recurse bool `yaml:"recurse"`
	} `yaml:"directory"`
}

// SyncOrder returns the Applications to sync after a render, in order: the
// root App (it creates or updates the Applications themselves), the rendered
// Applications of the lowest sync order (the central security-operations App,
// which creates the tenant namespaces), the sealed-secrets App (the tenant
// Secrets need those namespaces), then every other rendered Application by
// sync order and name.
//
// The order comes from the kubeaid.io/sync-order labels of what the render
// wrote, so a change to the render's ordering is followed without a change
// here.
//
// TODO(kubesoc-07): once the GitOps branch lands, take the order from
// core.SecurityOperationsApplicationsInSyncOrder() (and its securityOperations.sync
// config) instead, and keep this as the fallback for a directory rendered by an
// older release.
func SyncOrder(apps []Application) []string {
	sorted := append([]Application(nil), apps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Order != sorted[j].Order {
			return sorted[i].Order < sorted[j].Order
		}
		return sorted[i].Name < sorted[j].Name
	})

	order := []string{constants.ArgoCDAppRoot}
	secretsAdded := false
	for i, app := range sorted {
		if i > 0 && !secretsAdded && app.Order != sorted[0].Order {
			order = append(order, constants.ArgoCDAppSealedSecrets)
			secretsAdded = true
		}
		order = append(order, app.Name)
	}
	if !secretsAdded {
		order = append(order, constants.ArgoCDAppSealedSecrets)
	}
	return order
}

// DryRunDiff renders into a scratch copy of clusterDir and returns the
// unified diff against clusterDir (empty when nothing would change). The
// cluster directory is not touched.
func DryRunDiff(ctx context.Context, clusterDir string, render RenderFunc) (string, []string, error) {
	scratch, err := os.MkdirTemp("", "kubesoc-dry-run-")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	copyDir := filepath.Join(scratch, "b")
	if err := CopyTree(clusterDir, copyDir); err != nil {
		return "", nil, fmt.Errorf("copying %s: %w", clusterDir, err)
	}
	written, err := render(ctx, copyDir)
	if err != nil {
		return "", written, err
	}
	diff, err := DiffTrees(ctx, clusterDir, copyDir)
	return diff, written, err
}

// DiffTrees returns `git diff --no-index` of two directories, with both
// roots shown as a/ and b/.
func DiffTrees(ctx context.Context, a, b string) (string, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return "", err
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return "", err
	}
	//nolint:gosec // G204: git with fixed arguments and two directory paths.
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-color", "--", absA, absB)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return "", fmt.Errorf("git diff: %w: %s", err, stderr.String())
	}
	s := out.String()
	s = strings.ReplaceAll(s, strings.TrimPrefix(absA, "/")+"/", "")
	s = strings.ReplaceAll(s, strings.TrimPrefix(absB, "/")+"/", "")
	return s, nil
}

// CopyTree copies the regular files under src to dst, skipping .git.
func CopyTree(src, dst string) error {
	//nolint:gosec // G122/G703: both trees are the operator's own checkout and a
	// temporary copy of it; the files are copied verbatim.
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, info.Mode().Perm())
	})
}

// Git runs git in a repository through the git binary, so the operator's own
// configuration (SSH agent, commit signing, credential helpers) applies.
type Git struct {
	Dir string
	// Run executes git with args in Dir; nil means the git binary.
	Run func(ctx context.Context, dir string, args ...string) (string, error)
}

func (g Git) run(ctx context.Context, args ...string) (string, error) {
	if g.Run != nil {
		return g.Run(ctx, g.Dir, args...)
	}
	//nolint:gosec // G204: git with the arguments this package builds itself.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// TopLevel returns the repository root.
func (g Git) TopLevel(ctx context.Context) (string, error) {
	return g.run(ctx, "rev-parse", "--show-toplevel")
}

// CurrentBranch returns the checked out branch.
func (g Git) CurrentBranch(ctx context.Context) (string, error) {
	return g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
}

// Dirty lists uncommitted changes under paths (git status --porcelain).
func (g Git) Dirty(ctx context.Context, paths ...string) (string, error) {
	return g.run(ctx, append([]string{gitStatus, "--porcelain", "--"}, paths...)...)
}

// CheckoutBranch switches to branch, creating it from HEAD when it does not
// exist. Local changes are carried over (never discarded).
func (g Git) CheckoutBranch(ctx context.Context, branch string) error {
	if _, err := g.run(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = g.run(ctx, "checkout", branch)
		return err
	}
	_, err := g.run(ctx, "checkout", "-b", branch)
	return err
}

// Commit stages exactly paths (additions, changes and deletions) and commits
// them. Returns "" when there was nothing to commit.
func (g Git) Commit(ctx context.Context, message string, paths ...string) (string, error) {
	if _, err := g.run(ctx, append([]string{"add", "--all", "--"}, paths...)...); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, append([]string{"diff", "--cached", "--quiet", "--"}, paths...)...); err == nil {
		return "", nil
	}
	if _, err := g.run(ctx, append([]string{"commit", "-m", message, "--"}, paths...)...); err != nil {
		return "", err
	}
	return g.run(ctx, "rev-parse", "HEAD")
}

// Push pushes branch to origin (never forced).
func (g Git) Push(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "push", "--set-upstream", "origin", branch)
	return err
}

// OriginURL returns origin's URL.
func (g Git) OriginURL(ctx context.Context) (string, error) {
	return g.run(ctx, "remote", "get-url", "origin")
}

// DefaultBranchName is the branch an apply commits to when none is given:
// kubesoc/<verb>-<UTC timestamp>.
func DefaultBranchName(verb string, now time.Time) string {
	return fmt.Sprintf("kubesoc/%s-%s", verb, now.UTC().Format("20060102-150405"))
}

// CompareURL returns the web page for opening a pull request of branch
// against base on common forges, "" when the remote is not recognised.
func CompareURL(remote, base, branch string) string {
	u := strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(u, "git@"):
		// git@host:owner/repo
		u = "https://" + strings.Replace(strings.TrimPrefix(u, "git@"), ":", "/", 1)
	case strings.HasPrefix(u, "ssh://"):
		u = strings.TrimPrefix(u, "ssh://")
		u = strings.TrimPrefix(u, "git@")
		if host, rest, ok := strings.Cut(u, "/"); ok {
			host, _, _ = strings.Cut(host, ":")
			u = "https://" + host + "/" + rest
		}
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
	default:
		return ""
	}
	if strings.Contains(u, "gitlab") {
		return fmt.Sprintf("%s/-/merge_requests/new?merge_request[source_branch]=%s&merge_request[target_branch]=%s",
			u, branch, base)
	}
	return fmt.Sprintf("%s/compare/%s...%s", u, base, branch)
}
