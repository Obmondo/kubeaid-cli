# Release

Versions are managed by [cocogitto](https://docs.cocogitto.io/) (`cog`) from Conventional Commit history. A release is produced by running `cog bump`, which derives the next version from commits since the last tag, updates `CHANGELOG.md`, creates a release commit, tags it, and pushes the tag. There is no `version.txt` — the version is the tag, and `goreleaser` injects it into the binaries via `-ldflags` at build time.

## Installing cog

One-time setup on your host. Pick one:

```bash
# Static binary (all platforms) — pinned version
curl -fsSL https://github.com/cocogitto/cocogitto/releases/download/7.0.0/cocogitto-7.0.0-x86_64-unknown-linux-musl.tar.gz \
  | tar xz -C /tmp
install -m 755 /tmp/x86_64-unknown-linux-musl/cog ~/bin/cog

# macOS
brew install cocogitto

# Cargo (from source)
cargo install cocogitto
```

Swap `x86_64-unknown-linux-musl` for `aarch64-unknown-linux-gnu` on ARM64 Linux, or `aarch64-apple-darwin` / `x86_64-apple-darwin` on macOS.

## Cutting a release

From a clean `main`:

```bash
cog bump --auto            # or --patch / --minor / --major / --version X.Y.Z
```

`cog` reads `cog.toml` and runs:

1. **Pre-bump hooks** — `make run-generators` rebuilds generated config artifacts, then `sed` updates the version pin in `flake.nix`.
   > The (commented-out) `nix-update` step that refreshes `vendorHash` takes a few minutes — enable only when a Nix-install path needs to work for that release.

2. **Bump** — writes the new version into `CHANGELOG.md`, creates the release commit (`chore(version): vX.Y.Z`), and creates the tag `vX.Y.Z`.

3. **Post-bump hooks** — pushes the tag to both remotes: `origin` (Gitea) and `github` (the public mirror). The GitHub push is what fires `.github/workflows/release.yaml` to build binaries and container images.

Merge commits are skipped when deriving the next version (`ignore_merge_commits = true` in `cog.toml`).

## What CI does on tag push

Four jobs run in parallel from `.github/workflows/release.yaml`:

| Job | Does |
|---|---|
| **Security scan** (`scan_sourcecode`) | Trivy scans the source tree. Findings are appended to the workflow run summary. |
| **Release notes** (`create-release`) | Generates release notes from the tag range with `cog changelog --at <tag>`, then creates the GitHub release via `softprops/action-gh-release`. |
| **Container images** (`build_kubeaid_core_per_arch` + `publish_kubeaid_core_manifest`) | Builds KubeAid Core natively on per-arch runners (`ubuntu-24.04` for amd64, `ubuntu-24.04-arm` for arm64), pushes each by digest, then stitches them into a multi-arch manifest at `ghcr.io/obmondo/kubeaid-core:<tag>`. |
| **Binaries + packages** (`build_and_publish_kubeaid_cli_binaries`) | `goreleaser -f .goreleaser-github.yaml` produces `kubeaid-cli` and `kubeaid-storagectl` for darwin/linux × amd64/arm64, packages them as `.deb` / `.rpm` / `.apk` / `archlinux` via `nfpms`, and attaches everything to the release. |
