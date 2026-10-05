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
  required_version = ">= 1.16.0"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 8.5.0"
    }
  }
}

provider "google" {
  project                     = var.project_id
  region                      = var.region
  zone                        = var.zone
  impersonate_service_account = var.provisioning_sa_email
}
```

`backend.tf` is an empty stub; the bucket/prefix/impersonation are supplied at
`init` time, not hardcoded:

```hcl
terraform {
  backend "gcs" {}
}
```

`variables.tf` should expose at least `project_id`, `provisioning_sa_email`,
`region`, and `zone` (without hardcoded defaults, as `project-setup.sh` writes
them into each module's `terraform.tfvars`), matching the other modules.

## Init and apply pattern

The module is initialized against the shared Terraform state bucket with an
impersonated provisioning service account and a module-specific state prefix:

```bash
terraform -chdir=terraform/<name> init -upgrade \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=<name>/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"
```

## Register the module for CI validation

CI validates each module by initializing it with `-backend=false` and running
`terraform validate`. The module list is in the "Terraform Init Mocks" step of
`.github/workflows/pr-validations.yml`. If the new module should be validated in
CI, add its path to that loop. Add a corresponding `.tftest.hcl` suite under
`terraform/tests/` and wire the module in `scripts/run-unit-tests.sh`.

## Validate

Follow [validate.md](validate.md). License headers are inserted automatically by
the `addlicense` pre-commit hook. To check locally without a backend:

```bash
terraform -chdir=terraform/<name> init -upgrade -backend=false
terraform -chdir=terraform/<name> validate
```

Never run `terraform apply` to validate, as it provisions billable
infrastructure.
