# Contributing to KubeAid CLI

Thank you for your interest in contributing! This project follows the
[CNCF Community Code of Conduct](CODE_OF_CONDUCT.md) — please read it before
participating.

## Before you start

- For anything beyond a small fix, open an [issue](https://github.com/Obmondo/kubeaid-cli/issues)
  describing the bug or feature first, so the approach can be discussed before
  you invest time in an implementation.
- Check [GOVERNANCE.md](GOVERNANCE.md) for how decisions get made and who the
  current [maintainers](MAINTAINERS.md) are.

## Making a change

1. Fork the repository and create a branch off `main`.
2. Follow Google's [Go style guide](https://google.github.io/styleguide/go/decisions).
3. Run the local checks before pushing — CI enforces all of these:

   ```sh
   make format   # golangci-lint fmt
   make lint     # golangci-lint run ./...
   make test     # go test ./...
   make check-coverage
   ```

4. Add or update tests for any behavior change. Golden-file tests live under
   `testdata/golden` — regenerate them with `make -C pkg/config/prompt
   update-golden` style flags documented next to the test, never hand-edit.
5. If you touched a Go file that doesn't yet have a license header, run
   `make addlicense`.
6. Write commit messages as [Conventional Commits](https://www.conventionalcommits.org/)
   (`fix:`, `feat:`, `chore:`, etc.) — releases are cut from these via
   [cocogitto](https://docs.cocogitto.io/), and CI validates commit format on
   every pull request.

## Sign off your commits

Every commit must include a `Signed-off-by` trailer, certifying you wrote it
or otherwise have the right to submit it under this project's license
(the [Developer Certificate of Origin](https://developercertificate.org/)).
Git adds this for you automatically with `-s`:

```sh
git commit -s -m "fix: correct the thing"
```

## Opening the pull request

- Reference the issue it addresses.
- Explain the *why*, not just the *what* — the diff already shows what
  changed.
- Keep the change scoped to the issue; unrelated cleanup belongs in its own
  PR.
- A maintainer will review and merge once CI is green and the change has been
  approved.

## Reporting a security issue

Please do not open a public issue for a security vulnerability. See
[SECURITY.md](SECURITY.md) for how to report it privately.
