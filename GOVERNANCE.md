# KubeAid CLI Governance

This document defines governance policies for the KubeAid CLI project.

## Our Mission

> **Operate the full lifecycle of Kubernetes clusters, the GitOps-native way — anywhere.**

Bootstrapping and operating production Kubernetes is still harder than it should
be, especially across more than one cloud, or on bare metal. Our goal is to give
teams a single, consistent way to bootstrap, upgrade, recover, test, and delete
clusters — on AWS, Azure, Hetzner, or generic bare metal — without hand-rolling
Cluster API manifests or juggling provider-specific tooling.

KubeAid CLI aims to make GitOps the default operating model for cluster
lifecycle management, not an advanced add-on: every change it makes lands as a
Git commit, reconciled by ArgoCD, so a cluster's entire history is auditable and
reproducible.

## Core Values

We believe a healthy open-source project is built on trust, merit, and
accountability. KubeAid CLI embraces the following values:

* **Technical Excellence:** We aim to make the safest, most predictable path
  to a production Kubernetes cluster available on any supported provider.
* **Security and Quality by Design:** Tests, linting, and validation are part
  of the development lifecycle, not an afterthought — see `make lint`,
  `make test`, and `make check-coverage`.
* **Community First:** The health of the project and its users comes before
  the release schedule or goals of any single sponsoring organization. Every
  contributor participates as an individual peer.
* **Fairness and Meritocracy:** Contributions are evaluated on their technical
  merit and value to the project, regardless of the contributor's employer.
* **Radical Transparency:** Development happens in the open — issues, pull
  requests, and design discussions are the default venue for decisions.

## Maintainers

Maintainers are the stewards of KubeAid CLI. Being a maintainer is a
privilege that comes with responsibility — it is reserved for individuals who
have demonstrated sustained commitment to the project's health and growth.

A maintainer is more than a contributor with write access. They are expected
to:

* Collaborate effectively with the wider community.
* Facilitate reviews by connecting contributors with the right expertise.
* Uphold high standards for code quality and test coverage.
* Take ownership of issues through to resolution.

All current maintainers are listed in [MAINTAINERS.md](MAINTAINERS.md).

### Teams and review

Maintainers are responsible for the whole project. The teams below give people
a clear first place to review and maintain. They are not exclusive ownership
and they do not change repository permissions.

* **Core maintainers:** shared design, configuration rendering, GitOps and
  Argo CD integration, governance, and architecture decisions.
* **Provider teams:** AWS/CAPA, Azure/CAPZ, Hetzner/CAPH, and generic bare
  metal/KubeOne code and tests.
* **Technical team:** shared tooling, tests, security, and cross-provider
  work.
* **Documentation and release team:** user and contributor documentation,
  release notes, and release-process changes.

Ask for review from the team affected by a change. A core maintainer must also
review changes to shared configuration or lifecycle behavior, GitOps/Argo CD,
security-sensitive code, or more than one provider.

### Changes in Leadership

The maintainer group is self-governing. New maintainers must be nominated by
an existing maintainer. Both appointments and removals are decided by a
**two-thirds (⅔) majority vote** of current maintainers.

Team assignments are recorded in [MAINTAINERS.md](MAINTAINERS.md). Core
maintainers review assignment changes and announce them publicly.

People listed as maintainer nominees are awaiting the required vote. They are
not maintainers and do not receive maintainer permissions before approval.

A maintainer who steps down, or is removed by vote, is acknowledged for their
past contributions in the project's history and release notes.

### GitHub Permissions

* **Maintainers:** Have write access to the repository — managing issues,
  reviewing pull requests, and merging code.
* **Release management:** Cutting releases (`cocogitto` + `goreleaser`) is
  restricted to maintainers, since it publishes signed binaries and packages.

## Communication

Discussion and design decisions happen in GitHub issues and pull requests by
default. Anything sensitive — an unpatched security vulnerability, or a Code
of Conduct report — is instead handled privately among maintainers, as
described below.

## Code of Conduct Enforcement

KubeAid CLI follows the [CNCF Community Code of Conduct](CODE_OF_CONDUCT.md).
Reports concerning general community members are reviewed and resolved by the
maintainers.

**Reports against a maintainer:** the maintainer in question is recused from
the discussion entirely. The remaining maintainers designate someone to
oversee the resolution, involving an independent third party if needed to
keep the process fair.

## Decision Making

### Lazy Consensus

Most day-to-day changes operate on lazy consensus: a proposal or change is
assumed accepted unless an objection is raised within a reasonable timeframe.
This keeps the project moving without unnecessary process.

### Voting

Any maintainer may call for a formal vote on a specific decision, held in a
GitHub Discussion, a maintainers-only channel (for security or conduct
matters), or during a live discussion.

* **Standard decisions** require a **simple majority (>50%)** of active
  maintainers.
* **Changes to this governance document** require a **two-thirds (⅔)
  supermajority** of active maintainers.
