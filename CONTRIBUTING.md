# Contributing

Want to contribute? Great! First, read this page.

## Contributor License Agreement

Contributions to this project must be accompanied by a Contributor License
Agreement. You (or your employer) retain the copyright to your contribution;
this simply gives us permission to use and redistribute your contributions as
part of the project. Head over to <https://cla.developers.google.com/> to see
your current agreements on file or to sign a new one.

You generally only need to submit a CLA once, so if you've already submitted one
(even if it was for a different project), you probably don't need to do it
again.

## AI tool Use

We expect our contributors to embrace AI tools.

This policy ensures that AI-generated contributions are always guided,
validated, and owned by a skilled human, promoting better software and a
stronger community than policing how code gets written.

### Policy

While contributors can use any AI tools to create their contributions, **human
oversight is mandatory**. All AI-generated content must be thoroughly reviewed
by the contributor before being submitted for review. The contributor remains
the sole author and is fully responsible for the contribution's accuracy,
quality, and maintainability. **AI should augment your abilities, not replace
your critical judgment.** Contributors should ensure their work meets a high
standard before seeking review, respecting maintainers' time, and must be
**prepared to discuss and justify their contributions** during the review
process.

All submissions to Google Open Source projects need to follow Google’s
Contributor License Agreement (CLA), which covers any original work of
authorship included in the submission. This doesn’t prohibit the use of coding
assistance tools, including tool-, AI-, or machine-generated code, as long as
these submissions abide by the CLA's requirements.

## Local Development Environment Setup

GEM is a multi-language, infrastructure-as-code project built from Terraform,
Ansible, Go, Python, and Bash. The feature you work on determines which
toolchain you need, but the linters and the pre-commit test hooks run across all
of them. It is recommended to install everything once rather than piecemeal.

The tooling used for local development does not need a GCP project, credentials,
or network access to Google Cloud. The linters and the unit tests run against
mocked providers and local playbook renders, and they create no infrastructure.

You only need the development tools for GEM in
[Development tooling for GEM environments](#development-tooling-for-gem-environments)
when you provision an actual GEM environment.

Linux and macOS are both supported as development machines.

### Required tooling

| Tool         | Version                                                      | Used for                                                                                                                               |
| :----------- | :----------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------------------- |
| `python3`    | 3.13 or higher                                               | `pre-commit`, Ansible, and the `api/` service (`api/pyproject.toml` sets `requires-python = ">=3.13"`)                                 |
| `pre-commit` | 4.0 or higher                                                | All pre-commit checks                                                                                                                  |
| `terraform`  | 1.14.0 or higher                                             | `terraform/` modules, the `terraform_fmt`, `terraform_tflint`, and `terraform_validate` pre-commit hooks, and the Terraform unit tests |
| `tflint`     | 0.50.0 or higher                                             | The `terraform_tflint` hook                                                                                                            |
| `ansible`    | Current release of the `ansible` package, not `ansible-core` | `ansible/` playbooks and roles, the `ansible-lint` pre-commit hook, and the Ansible unit tests                                         |
| `go`         | 1.26.6 or higher                                             | `operators/gem-network-operator`, and the Go-based pre-commit hooks                                                                    |
| `uv`         | Current release                                              | GEM REST API, `ruff`, and `pytest`                                                                                                     |
| `shellcheck` | Any recent release                                           | The `shellcheck` pre-commmit hook                                                                                                      |
| `jq`         | Any recent release                                           | `ansible/inventory.sh`, the dynamic inventory                                                                                          |

> [!IMPORTANT]
> You need Go installed on your development machine even if you never touch Go
> code. `pre-commit` builds `yamlfmt`, `gitleaks`, `addlicense`, and
> `golangci-lint` from source using the `go` binary on your `PATH`, and it will
> not install Go for you. Many of these run on every commit, including
> documentation-only commits.

### Install the tooling

#### On Debian or Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y ansible ansible-lint python3 python3-pip python3-venv golang-go shellcheck jq
```

Install `uv` using the
[official installer](https://docs.astral.sh/uv/getting-started/installation/):

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

#### On macOS with Homebrew:

```bash
brew install python@3.13 go shellcheck jq uv
```

Install `terraform` and `tflint` from their upstream releases::

- [Terraform](https://developer.hashicorp.com/terraform/install)
- [TFLint](https://github.com/terraform-linters/tflint#installation)

Install the Python tools in a way that keeps them isolated in a virtual
environment. `uv tool` and `pipx` both work, `uv` is preferred and used
throughout this documentation:

```bash
for tool in pre-commit ansible; do
  uv tool install ${tool}
done

uv tool install mdformat \
--with mdformat-gfm \
--with mdformat-frontmatter \
--with mdformat-gfm-alerts
```

Confirm that your distribution's Go build satisfies the minimum Golang version
`go 1.26.6`. An older version will fail to build the operator:

```bash
go version

# macOS
go version go1.27.1 darwin/arm64

# Linux
go version go1.26.6 linux/amd64
```

Distribution packages often lag. If your Go version is older, install a current
release from [go.dev/dl](https://go.dev/dl/) and put it ahead of the system Go
on your `PATH`.

### Install the Git hooks

Clone the repository, then install the hooks from the repository root:

```bash
cd ${REPO_ROOT}
pre-commit install --install-hooks
pre-commit install --hook-type commit-msg --hook-type pre-push
```

Both commands are necessary as `.pre-commit-config.yaml` registers hooks at
three Git stages:

| Stage        | Hooks                                                                                                                                                                      |
| :----------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pre-commit` | Whitespace, YAML lint and format, Markdown format, secret scanning, Terraform format/lint/validate, `ansible-lint`, `ruff`, `golangci-lint`, license headers, `shellcheck` |
| `commit-msg` | `conventional-pre-commit`, which enforces [Conventional Commits](https://www.conventionalcommits.org/) so `release-please` can build the changelog                         |
| `pre-push`   | The unit-test suites in `scripts/run-unit-tests.sh`                                                                                                                        |

### Initialize the Terraform modules

The `terraform_validate` and `terraform_tflint` hooks need each module's
providers downloaded. Initialize all five without touching remote state:

```bash
cd ${REPO_ROOT}
for dir in terraform/foundation terraform/admin-workstation terraform/cluster \
           terraform/edge-router terraform/cloudbuild; do
  terraform -chdir="${dir}" init -backend=false
done
```

`-backend=false` skips the GCS backend declared in each module's `backend.tf`,
so this works on a fresh clone with no bucket and no credentials.

### Verify your setup

Run the full lint pass and the full unit-test suite:

```bash
cd ${REPO_ROOT}
pre-commit run --all-files
./scripts/run-unit-tests.sh
```

`scripts/run-unit-tests.sh` checks for the tools each suite needs before it runs
and exits with detail on the missing tool if one is absent. This makes it a
usable installation check on its own. It runs four test suites:

| Flag          | Suite                                                                                                                   |
| :------------ | :---------------------------------------------------------------------------------------------------------------------- |
| `--terraform` | `terraform test` against a temporary copy of `terraform/cluster`, using `mock_provider "google"`                        |
| `--ansible`   | Three check-mode playbooks in `ansible/tests/`: template rendering, parameter validation, and VXLAN interface rendering |
| `--go`        | `go test -v -cover ./...` in `operators/gem-network-operator`                                                           |
| `--python`    | `uv run pytest -v --cov=gem_api` in `api/`                                                                              |
| `--all`       | All four, which is also the default when you pass no flags                                                              |

While iterating on one area, run just that suite:

```bash
./scripts/run-unit-tests.sh --go
```

You can also run either the Python or Go test suites directly:

```bash
# Python
cd api && uv sync && uv run pytest -v

# Go
cd operators/gem-network-operator && go test ./...
```

To skip a slow or irrelevant hook for a single run, use the `SKIP` env variable.

```bash
SKIP=terraform_validate,golangci-lint pre-commit run --all-files
```

### Development tooling for GEM environments

You need these tools to provision or interact with a real GEM environment. These
tools are not needed to run linters or unit tests:

- [Google Cloud SDK](https://cloud.google.com/sdk) (`gcloud`), authenticated,
  with
  [Application Default Credentials](https://docs.cloud.google.com/docs/authentication/application-default-credentials)
  configured. `ansible/inventory.sh` reads Terraform state from GCS through it
  and tunnels to hosts over IAP.
- `kubectl`, to talk to a provisioned GEM cluster.
- [Kyverno Chainsaw](https://kyverno.github.io/chainsaw/), to run the end-to-end
  suites in `tests/e2e/`. These require a running cluster.

See [Getting Started](README.md#getting-started) for the provisioning workflow
and [docs/project-setup.md](docs/project-setup.md) for GCP project
configuration.

### Troubleshooting

| Symptom                                                             | Cause                                                                                      | Fix                                                                                                       |
| :------------------------------------------------------------------ | :----------------------------------------------------------------------------------------- | :-------------------------------------------------------------------------------------------------------- |
| `Missing required tool: <name>` from `run-unit-tests.sh`            | The suite you selected needs a tool that is not on your `PATH`                             | Install it from the table above, or run only the suites you have tooling for                              |
| A pre-commit hook fails with `go: command not found` during install | No Go toolchain on `PATH`                                                                  | Install Go 1.26.6 or higher, then `pre-commit install --install-hooks` again                              |
| `couldn't resolve module/action 'ansible.posix.sysctl'`             | You installed `ansible-core` instead of `ansible`                                          | `pip uninstall ansible-core && uv tool install ansible`                                                   |
| `terraform_validate` fails on a fresh clone                         | The module has no `.terraform` directory yet                                               | Run the initialization loop in [Initialize the Terraform modules](#initialize-the-terraform-modules)      |
| Your commit is rejected before the editor opens                     | The message is not a [Conventional Commit](https://www.conventionalcommits.org/en/v1.0.0/) | Prefix the subject with a type, for example `docs(contributing): add detail on conventional commits`      |
| Unit tests run on `git push` and you did not expect it              | The pre-push hooks are installed and working as intended                                   | Run `./scripts/run-unit-tests.sh` before pushing, or `git push --no-verify` for a work-in-progress branch |
| The first `pre-commit run --all-files` takes several minutes        | Hook environments are being built, including four Go binaries                              | Wait it out once. Later runs read from `~/.cache/pre-commit`                                              |

## Contribution process

### Before you open a pull request

- Ensure that all pre-commits pass. The same precommit checks are run through
  Github workflows for each PR, and subsequent push to an open PR. These
  pre-commit checks run automatically if you've already
  [installed the Git hooks](#install-the-git-hooks). Run the same checks CI
  runs:

- If your feature or bug fix changes substantial functionality, ensure the
  project documentation is updated to match.

### Code reviews

All submissions, including those from project members, require review. Code
changes are accepted through
[GitHub pull requests](https://docs.github.com/articles/about-pull-requests).

Give the pull request a
[Conventional Commits](https://www.conventionalcommits.org/) title. A
non-conforming title will fails presubmit checks.

When approved, a team member submits the change and it merges automatically.

## Review our Community Guidelines

This project follows
[Google's Open Source Community Guidelines](https://opensource.google/conduct/).
