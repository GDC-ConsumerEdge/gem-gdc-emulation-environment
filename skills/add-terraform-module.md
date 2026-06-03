# Add a Terraform module

GEM provisions infrastructure with independent Terraform root modules under
`terraform/<name>/` (`foundation`, `admin-workstation`, `cluster`,
`edge-router`, `cloudbuild`). Each uses a GCS backend and a service-account
impersonation pattern. Match an existing module rather than inventing a layout.

## File convention

Every module uses the same base files, each with the Apache license header:

```
terraform/<name>/
  main.tf         # terraform{} block + provider config
  variables.tf    # input variables
  outputs.tf      # outputs consumed by other modules / Ansible
  backend.tf      # GCS backend stub
  <name>.tf       # the actual resources (e.g. cluster-nodes.tf)
```

`main.tf` pins the required version and the Google provider:

```hcl
terraform {
  required_version = ">= 1.12.2"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.30.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}
```

`backend.tf` is an empty stub; the bucket/prefix/impersonation are supplied at
`init` time, not hardcoded:

```hcl
terraform {
  backend "gcs" {}
}
```

`variables.tf` should expose at least `project_id`, `region` (default
`us-central1`), and `zone` (default `us-central1-a`), matching the other modules.

## Init and apply pattern

The module is initialized against the shared state bucket with an impersonated
provisioning service account and a module-specific state prefix:

```bash
terraform -chdir=terraform/<name> init \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=<name>/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"
```

## Register the module for CI validation

CI validates each module by stripping its backend and running `terraform
validate`. The module list is hardcoded in the "Terraform Init Mocks" step of
`.github/workflows/pr-validations.yml`. If the new module should be validated in
CI, add its path to that loop. If it needs unit tests, follow the pattern in
`terraform/tests/` and `scripts/run-unit-tests.sh`.

## Validate

Follow [validate.md](validate.md). License headers are inserted automatically by
the `addlicense` pre-commit hook. To check locally without a backend:

```bash
terraform -chdir=terraform/<name> init -backend=false
terraform -chdir=terraform/<name> validate
```

Never run `terraform apply` to validate — it provisions billable infrastructure.
