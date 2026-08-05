# Contributing to KubeAid CLI

Thanks for taking the time to contribute. This document covers the practical
steps; for how decisions get made and who has merge rights, see
[GOVERNANCE.md](GOVERNANCE.md) and [MAINTAINERS.md](MAINTAINERS.md). Everyone
participating is expected to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

Open an [issue](https://github.com/Obmondo/kubeaid-cli/issues) describing the
bug or feature before starting substantial work — it avoids duplicated effort
and gives maintainers a chance to weigh in on approach before you've written
the code.

## Development setup

```bash
git clone https://github.com/Obmondo/kubeaid-cli.git
cd kubeaid-cli

make build              # build the kubeaid-cli binary
make build-storagectl   # build the kubeaid-storagectl binary (bare-metal storage helper)
make lint               # Go linters
make format             # formatter checks
make test               # unit tests, writes coverage.out
make check-coverage     # enforce testcoverage.yaml thresholds
```

Run `make help` to list every target. See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)
for a deeper walkthrough.

## Code standards

* Follow Google's [Go style guide](https://google.github.io/styleguide/go/decisions).
* `make lint` and `make format` must be clean before pushing — CI enforces
  both on every pull request, along with `make check-coverage` (per-package
  thresholds are defined in `testcoverage.yaml`, not a single blanket number).
* New code should come with tests; if a package already has a coverage
  threshold in `testcoverage.yaml`, don't lower it.

## Commit messages

Write [Conventional Commits](https://www.conventionalcommits.org/) —
releases are cut with [cocogitto](https://docs.cocogitto.io/) directly from
commit history, so a malformed type/scope either fails release tooling or
misfiles your change in the changelog. See [docs/release.md](docs/release.md)
for how versioning works end to end.

## Pull requests

1. Reference the issue the PR addresses.
2. Explain the *why* in the description, not just the *what* — the diff
   already shows what changed.
3. Keep PRs scoped to one change; unrelated cleanup belongs in its own PR.
4. A maintainer will review and merge — see
   [MAINTAINERS.md](MAINTAINERS.md) for who that is.

## Reporting a security issue

Don't open a public issue for a vulnerability — see
[SECURITY.md](SECURITY.md) for how to report it privately.
