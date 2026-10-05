# Validate a change

How to check a GEM change locally without provisioning real infrastructure. GEM
is built from Terraform, Ansible, Go, Python, Bash, and Cloud Build, so
validation means linting, formatting, and **dry** unit tests, never creating or
mutating real GCP resources. The same procedure runs in CI
(`.github/workflows/pr-validations.yml`).

## Hard rule

Never run `terraform apply`, `terraform destroy`, or a live Ansible play
(`create-cluster.yaml`, `edge-router.yaml`, `cleanup.yaml`,
`admin-workstation.yaml`, `restore-vxlan.yaml`, etc.) to validate a change.
These provision or mutate billable infrastructure. Validation is read-only.

## Steps

1. Lint and format with pre-commit, on the changed files (faster) or all:

   ```bash
   pre-commit run --files <changed files...>
   # or
   pre-commit run --all-files
   ```

   This covers trailing whitespace, end-of-file, YAML lint/format, Markdown
   format, secret scanning (gitleaks), license headers (addlicense, which
   inserts missing headers automatically), shellcheck, ansible-lint, ruff,
   golangci-lint, and Terraform `fmt`/`validate`/`tflint`. The output must be
   pristine.

2. Run the project unit tests:

   ```bash
   ./scripts/run-unit-tests.sh
   ```

   This runs four offline suites:

   - `--terraform`: copies each module under `terraform/` to a temporary
     directory, strips `backend.tf` so the GCS backend is bypassed, and runs
     `terraform test`.
   - `--ansible`: runs the four check-mode playbooks under `ansible/tests/`
     (covering GDC cluster template rendering, parameter validation, VXLAN
     rendering, and component templates/helper scripts).
   - `--go`: runs `go test -v -cover ./...` in
     `operators/gem-network-operator/`.
   - `--python`: runs `uv run --frozen pytest -v --cov=gem_api` in `api/`.

   None of these suites contact GCP.

## Targeted validation

Validate only Terraform without a backend (mirrors the CI "Terraform Init Mocks"
step), per module:

```bash
terraform -chdir=terraform/<module> init -upgrade -backend=false
terraform -chdir=terraform/<module> validate
```

Run a single pre-commit hook, e.g. just Terraform validate or just ansible-lint:

```bash
pre-commit run terraform_validate --all-files
pre-commit run ansible-lint --all-files
```

## Interpreting results

Treat any non-pristine output as a failure to fix, not noise. Lint, format, and
test output frequently carry the actual problem. Report failures with their
output rather than summarizing them away.
