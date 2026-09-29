// Copyright 2026 Obmondo
// SPDX-License-Identifier: Apache-2.0

package siemctl

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/cli/values"

	"github.com/Obmondo/kubeaid-cli/pkg/utils/kubernetes"
)

// HelmRelease is one Argo CD Application as a local Helm release: the chart
// in a KubeAid checkout and the values files in the kubeaid-config checkout.
type HelmRelease struct {
	Name      string
	Namespace string
	// Chart is the chart's path in the KubeAid repository (e.g.
	// argocd-helm-charts/kubesoc/wazuh).
	Chart      string
	ChartPath  string
	ValueFiles []string
	// ValuesObject is the Application's inline helm.valuesObject.
	ValuesObject map[string]any
}

// HelmReleasesFromApps maps rendered Applications onto local checkouts:
// the source with a path is the chart (under kubeaidDir), and a
// $values/<path> value file is read from the kubeaid-config checkout that
// holds clusterDir (clusterDir is <config repo>/k8s/<cluster>).
func HelmReleasesFromApps(apps []Application, kubeaidDir, clusterDir string) ([]HelmRelease, error) {
	configRoot := filepath.Dir(filepath.Dir(clusterDir))
	var releases []HelmRelease
	for _, app := range apps {
		rel := HelmRelease{Name: app.Name, Namespace: app.Namespace}
		for _, s := range app.Sources {
			// A directory source is the Application's own sealed Secrets, not
			// a chart; QuickstartScript applies those with kubectl.
			if s.Path == "" || s.Directory {
				continue
			}
			rel.Chart = s.Path
			rel.ChartPath = filepath.Join(kubeaidDir, s.Path)
			rel.ValuesObject = s.ValuesObject
			for _, vf := range s.ValueFiles {
				p, ok := strings.CutPrefix(vf, "$values/")
				if !ok {
					p = filepath.Join(s.Path, vf)
					rel.ValueFiles = append(rel.ValueFiles, filepath.Join(kubeaidDir, p))
					continue
				}
				rel.ValueFiles = append(rel.ValueFiles, filepath.Join(configRoot, p))
			}
		}
		if rel.Chart == "" {
			return nil, fmt.Errorf("application %s has no chart source", app.Name)
		}
		releases = append(releases, rel)
	}
	return releases, nil
}

// RenderRelease runs `helm template` of a release in-process.
func RenderRelease(ctx context.Context, rel HelmRelease) (string, error) {
	files := append([]string(nil), rel.ValueFiles...)
	if len(rel.ValuesObject) > 0 {
		tmp, err := os.CreateTemp("", "kubesoc-values-*.yaml")
		if err != nil {
			return "", err
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if err := yaml.NewEncoder(tmp).Encode(rel.ValuesObject); err != nil {
			tmp.Close()
			return "", err
		}
		if err := tmp.Close(); err != nil {
			return "", err
		}
		files = append(files, tmp.Name())
	}
	return kubernetes.HelmRenderManifest(ctx, &kubernetes.HelmRenderArgs{
		ChartPath:   rel.ChartPath,
		ReleaseName: rel.Name,
		Namespace:   rel.Namespace,
		Values:      &values.Options{ValueFiles: files},
	})
}

// ExtractImages returns every container image a rendered manifest references,
// sorted and de-duplicated: `image:` strings (containers, init containers,
// CRDs such as MariaDB), `image: {registry, repository, tag}` maps, and CNPG's
// `imageName`.
func ExtractImages(manifest string) ([]string, error) {
	seen := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(manifest))
	for {
		var doc any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parsing rendered manifest: %w", err)
		}
		walkImages(doc, seen)
	}
	out := make([]string, 0, len(seen))
	for img := range seen {
		out = append(out, img)
	}
	sort.Strings(out)
	return out, nil
}

func walkImages(v any, seen map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch k {
			case "image", "imageName":
				if img := imageRef(child); img != "" {
					seen[img] = true
					continue
				}
			}
			walkImages(child, seen)
		}
	case []any:
		for _, child := range t {
			walkImages(child, seen)
		}
	}
}

// imageRef turns an image value into a reference, "" when it is not one.
func imageRef(v any) string {
	switch t := v.(type) {
	case string:
		s := strings.Trim(strings.TrimSpace(t), `"'`)
		if s == "" || strings.ContainsAny(s, " {}$\n") {
			return ""
		}
		return s
	case map[string]any:
		repo, _ := t["repository"].(string)
		if repo == "" {
			return ""
		}
		if reg, _ := t["registry"].(string); reg != "" {
			repo = strings.TrimSuffix(reg, "/") + "/" + repo
		}
		ref := repo
		if tag := fmt.Sprint(t["tag"]); t["tag"] != nil && tag != "" {
			ref += ":" + tag
		}
		if digest, _ := t["digest"].(string); digest != "" {
			ref += "@" + digest
		}
		return ref
	}
	return ""
}

// RewriteRef maps an image onto a private registry, keeping its repository
// path: docker.io/wazuh/wazuh-manager:4.14.3 becomes
// <registry>/wazuh/wazuh-manager:4.14.3. A digest reference keeps its digest.
func RewriteRef(image, registry string) (string, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return "", fmt.Errorf("parsing image %q: %w", image, err)
	}
	repo := ref.Context().RepositoryStr()
	dst := strings.TrimSuffix(registry, "/") + "/" + repo
	switch r := ref.(type) {
	case name.Digest:
		return dst + "@" + r.DigestStr(), nil
	case name.Tag:
		return dst + ":" + r.TagStr(), nil
	}
	return dst, nil
}

// dirMode is the mode of the directories a bundle is staged and unpacked in:
// charts and images an operator reads and hands to helm, no secrets.
const dirMode = 0o750

// defaultBundlePlatform is the image platform a bundle holds unless the
// operator asks for another.
const defaultBundlePlatform = "linux/amd64"

// refNameAnnotation is the OCI annotation the bundle keeps each image's
// original reference in.
const refNameAnnotation = "org.opencontainers.image.ref.name"

// BundleManifest is manifest.json at the root of a bundle.
type BundleManifest struct {
	Version       string          `json:"version"`
	ChartRevision string          `json:"chartRevision,omitempty"`
	Created       string          `json:"created"`
	Platform      string          `json:"platform"`
	Charts        []string        `json:"charts"`
	Images        []BundleImage   `json:"images"`
	Content       string          `json:"content,omitempty"`
	Models        []string        `json:"models,omitempty"`
	Releases      []BundleRelease `json:"releases"`
}

// BundleImage is one image in the bundle's OCI layout.
type BundleImage struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest,omitempty"`
}

// BundleRelease records which release referenced which images.
type BundleRelease struct {
	Name   string   `json:"name"`
	Chart  string   `json:"chart"`
	Images []string `json:"images"`
}

// BundleOptions are the inputs of BuildBundle.
type BundleOptions struct {
	Out           string
	Version       string
	ChartRevision string
	Releases      []HelmRelease
	// KubeaidDir is the KubeAid checkout the charts are copied from.
	KubeaidDir string
	// ContentDir is the content package (rules, decoders, dashboards) to
	// include; empty: none.
	ContentDir string
	// ModelsDir is an Ollama models directory (blobs/ and manifests/) to
	// include; empty: none.
	ModelsDir string
	Platform  string
	// ExtraImages are added to the rendered ones: images the manifests do not
	// name, such as an operator's default PostgreSQL or MariaDB image.
	ExtraImages []string
	// SkipImages writes the bundle without pulling images (manifest only).
	SkipImages bool
	// Render renders a release; nil means RenderRelease.
	Render func(ctx context.Context, rel HelmRelease) (string, error)
	// Pull fetches an image; nil means crane.Pull with the default keychain.
	Pull func(ref string, platform *v1.Platform) (v1.Image, error)
	Now  time.Time
	Log  io.Writer
}

// BuildBundle renders every release, collects its images, pulls them into
// an OCI layout and writes a tar with the charts, the images, the content
// package, the models and manifest.json.
func BuildBundle(ctx context.Context, opts BundleOptions) (*BundleManifest, error) {
	if opts.Platform == "" {
		opts.Platform = defaultBundlePlatform
	}
	platform, err := v1.ParsePlatform(opts.Platform)
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	stage, err := os.MkdirTemp("", "kubesoc-bundle-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	m := &BundleManifest{
		Version:       opts.Version,
		ChartRevision: opts.ChartRevision,
		Created:       now.UTC().Format(time.RFC3339),
		Platform:      opts.Platform,
	}

	refs, charts, err := renderReleases(ctx, opts, m)
	if err != nil {
		return nil, err
	}
	for _, chart := range charts {
		if err := copyFollowingLinks(filepath.Join(opts.KubeaidDir, chart), filepath.Join(stage, "charts", chart)); err != nil {
			return nil, fmt.Errorf("copying chart %s: %w", chart, err)
		}
	}
	m.Charts = charts

	if m.Images, err = collectImages(ctx, opts, stage, refs, platform); err != nil {
		return nil, err
	}

	if opts.ContentDir != "" {
		m.Content = "content"
		if err := copyFollowingLinks(opts.ContentDir, filepath.Join(stage, "content")); err != nil {
			return nil, fmt.Errorf("copying content package: %w", err)
		}
	}
	if opts.ModelsDir != "" {
		if err := copyFollowingLinks(opts.ModelsDir, filepath.Join(stage, "models")); err != nil {
			return nil, fmt.Errorf("copying models: %w", err)
		}
		m.Models = ollamaModels(filepath.Join(stage, "models"))
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), append(raw, '\n'), 0o600); err != nil {
		return nil, err
	}
	if err := tarDir(stage, opts.Out); err != nil {
		return nil, err
	}
	return m, nil
}

// renderReleases renders every release into m.Releases and returns the sorted
// images (the releases' plus opts.ExtraImages) and charts.
func renderReleases(ctx context.Context, opts BundleOptions, m *BundleManifest) (refs, charts []string, err error) {
	render := opts.Render
	if render == nil {
		render = RenderRelease
	}
	images := map[string]bool{}
	seenCharts := map[string]bool{}

	for _, rel := range opts.Releases {
		manifest, err := render(ctx, rel)
		if err != nil {
			return nil, nil, fmt.Errorf("rendering %s: %w", rel.Name, err)
		}
		relImages, err := ExtractImages(manifest)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", rel.Name, err)
		}
		logTo(opts.Log, "%s (%s): %d images", rel.Name, rel.Chart, len(relImages))
		m.Releases = append(m.Releases, BundleRelease{Name: rel.Name, Chart: rel.Chart, Images: relImages})
		for _, img := range relImages {
			images[img] = true
		}
		if !seenCharts[rel.Chart] {
			seenCharts[rel.Chart] = true
			charts = append(charts, rel.Chart)
		}
	}
	for _, img := range opts.ExtraImages {
		images[img] = true
	}

	refs = make([]string, 0, len(images))
	for img := range images {
		refs = append(refs, img)
	}
	sort.Strings(refs)
	sort.Strings(charts)
	return refs, charts, nil
}

// collectImages pulls every image into the bundle's OCI layout, or, with
// SkipImages, only lists them.
func collectImages(ctx context.Context, opts BundleOptions, stage string, refs []string, platform *v1.Platform) (
	[]BundleImage, error,
) {
	out := make([]BundleImage, 0, len(refs))
	if opts.SkipImages {
		for _, ref := range refs {
			out = append(out, BundleImage{Ref: ref, Digest: ""})
		}
		return out, nil
	}

	pull := opts.Pull
	if pull == nil {
		pull = func(ref string, p *v1.Platform) (v1.Image, error) {
			return crane.Pull(ref, crane.WithPlatform(p), crane.WithContext(ctx))
		}
	}
	lp, err := layout.Write(filepath.Join(stage, "images"), empty.Index)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		logTo(opts.Log, "pulling %s", ref)
		img, err := pull(ref, platform)
		if err != nil {
			return nil, fmt.Errorf("pulling %s: %w", ref, err)
		}
		digest, err := img.Digest()
		if err != nil {
			return nil, err
		}
		if err := lp.AppendImage(img, layout.WithAnnotations(map[string]string{
			refNameAnnotation: ref,
		})); err != nil {
			return nil, fmt.Errorf("writing %s: %w", ref, err)
		}
		out = append(out, BundleImage{Ref: ref, Digest: digest.String()})
	}
	return out, nil
}

// logTo writes a progress line when the caller wants one.
func logTo(w io.Writer, format string, a ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format+"\n", a...)
	}
}

// ollamaModels lists model names from an Ollama models tree
// (manifests/<registry>/<namespace>/<model>/<tag>).
func ollamaModels(dir string) []string {
	var models []string
	root := filepath.Join(dir, "manifests")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best effort listing
		}
		rel, _ := filepath.Rel(root, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 {
			models = append(models, parts[len(parts)-2]+":"+parts[len(parts)-1])
		}
		return nil
	})
	sort.Strings(models)
	return models
}

// ImportOptions are the inputs of ImportBundle.
type ImportOptions struct {
	Bundle   string
	Registry string
	Insecure bool
	// Push uploads an image; nil means crane.Push with the default keychain.
	Push func(img v1.Image, dst string) error
	Log  io.Writer
	// ExtractTo keeps the unpacked bundle (charts, content, models) there;
	// empty: a temporary directory that is removed.
	ExtractTo string
}

// ImportBundle unpacks a bundle and pushes every image to the registry,
// keeping repository paths. Returns the image mapping (source to target).
func ImportBundle(ctx context.Context, opts ImportOptions) (map[string]string, *BundleManifest, error) {
	dir := opts.ExtractTo
	if dir == "" {
		tmp, err := os.MkdirTemp("", "kubesoc-import-")
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		dir = tmp
	}
	if err := untar(opts.Bundle, dir); err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("not a kubesoc bundle: %w", err)
	}
	m := &BundleManifest{}
	if err := json.Unmarshal(raw, m); err != nil {
		return nil, nil, err
	}

	push := opts.Push
	if push == nil {
		craneOpts := []crane.Option{crane.WithContext(ctx)}
		if opts.Insecure {
			craneOpts = append(craneOpts, crane.Insecure)
		}
		push = func(img v1.Image, dst string) error { return crane.Push(img, dst, craneOpts...) }
	}

	mapping := map[string]string{}
	lp, err := layout.FromPath(filepath.Join(dir, "images"))
	if err != nil {
		if len(m.Images) > 0 && m.Images[0].Digest != "" {
			return nil, m, fmt.Errorf("reading the image layout: %w", err)
		}
		return mapping, m, nil // a --skip-images bundle
	}
	index, err := lp.ImageIndex()
	if err != nil {
		return nil, m, err
	}
	im, err := index.IndexManifest()
	if err != nil {
		return nil, m, err
	}
	for _, desc := range im.Manifests {
		src := desc.Annotations[refNameAnnotation]
		if src == "" {
			continue
		}
		dst, err := RewriteRef(src, opts.Registry)
		if err != nil {
			return mapping, m, err
		}
		// A digest reference cannot be pushed by tag; push under its repo.
		if strings.Contains(dst, "@") {
			dst = strings.SplitN(dst, "@", 2)[0] + ":" + strings.ReplaceAll(desc.Digest.String(), ":", "-")
		}
		img, err := lp.Image(desc.Digest)
		if err != nil {
			return mapping, m, err
		}
		if opts.Log != nil {
			_, _ = fmt.Fprintf(opts.Log, "pushing %s -> %s\n", src, dst)
		}
		if err := push(img, dst); err != nil {
			return mapping, m, fmt.Errorf("pushing %s: %w", dst, err)
		}
		mapping[src] = dst
	}
	return mapping, m, nil
}

// copyFollowingLinks copies src to dst, resolving symlinks (the umbrella
// chart links its sub-charts).
func copyFollowingLinks(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		raw, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
			return err
		}
		//nolint:gosec // G703: dst is inside the staging directory this builds.
		return os.WriteFile(dst, raw, info.Mode().Perm())
	}
	if err := os.MkdirAll(dst, dirMode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := copyFollowingLinks(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func tarDir(dir, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirMode); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(f)
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		//nolint:gosec // G122: p comes from walking our own staging directory.
		raw, err := os.Open(p)
		if err != nil {
			return err
		}
		defer raw.Close()
		_, err = io.Copy(tw, raw)
		return err
	})
	if walkErr != nil {
		_ = tw.Close()
		_ = f.Close()
		return walkErr
	}
	if err := tw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeFromReader(target string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil { //nolint:gosec // bundles are operator supplied
		out.Close()
		return err
	}
	return out.Close()
}

func untar(bundle, dir string) error {
	f, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(hdr.Name)) //nolint:gosec // checked below
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("bundle entry %q escapes the target directory", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, dirMode); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
				return err
			}
			if err := writeFromReader(target, tr, os.FileMode(hdr.Mode).Perm()); err != nil { //nolint:gosec // mode from our own bundle
				return err
			}
		}
	}
}
