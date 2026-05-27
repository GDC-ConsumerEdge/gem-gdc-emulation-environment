# Building GEM Clusters with Cloud Build

`cloudbuild/cluster-build.cloudbuild.yaml` is an on-demand pipeline that
provisions and configures a single GEM cluster end-to-end. This pipeline assumes
the Foundation and Admin Workstation are already deployed in the target
project. The Cloud Build pipeline only manages GEM clusters.



## One-time setup

You'll need `gcloud` authenticated as a user with the appropriate
permissions and able to impersonate `tf-provisioner@`. The standard GEM project
setup (`project-setup.sh`) must already be complete.

### Apply the Cloud Build Terraform module

This module creates all GCP resources required to create, manage and run this Cloud Build pipeline.

```bash
cd ${REPO_ROOT}/cloudbuild
./setup.sh
```

`setup.sh` is a thin wrapper that runs Terraform for you. Upon completion it prints the builder SA email, Artifact Registry URL, and SSH secret name
for use in subsequent steps.


### Build the Cloud Build builder image

This build step is required when the Cloud Build pipeline is created, and then again on Dockerfile changes:

```bash
gcloud builds submit \
  --config=${REPO_ROOT}/cloudbuild/builder/cloudbuild.yaml \
  ${REPO_ROOT}/cloudbuild/builder
```

## Submitting a build

```bash
gcloud builds submit \
  --config=cloudbuild/cluster-build.cloudbuild.yaml \
  --substitutions=_CLUSTER_NAME=gem-cluster-1 \
  --service-account=projects/${PROJECT_ID}/serviceAccounts/gem-cluster-builder@${PROJECT_ID}.iam.gserviceaccount.com \
  .
```

The `--service-account` flag is required: Cloud Build's default SA does not
have the permissions this pipeline needs. This pipeline is
designed to run as `gem-cluster-builder@`.

### Substitutions

| Substitution | Default | Purpose |
|---|---|---|
| `_CLUSTER_NAME` | `gem-cluster-1` | Cluster identifier; drives VM names, TF state prefix, fleet membership. Must be ≤26 chars (Kubernetes label limit). |
| `_TF_STATE_BUCKET` | `gem-${PROJECT_ID}-tfstate` | GCS bucket holding Terraform state. |
| `_EMULATE_GDC_VERSION` | _(latest)_ | Forwarded to Ansible as `emulate_gdc_version`. See `ansible/group_vars/all.yaml` for the supported set. |
| `_PROVISIONING_SA_EMAIL` | `tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com` | SA that Terraform impersonates for all provider calls. Empty resolves to this default in `setup.sh`; the build SA is deliberately under-privileged and cannot provision on its own. |
| `_DESTROY_ON_FAILURE` | `true` | Set to `false` to preserve a failed cluster for inspection. |
| `_AR_LOCATION` | `us-central1` | Artifact Registry location of the builder image. |
| `_AR_REPO` | `gem` | Artifact Registry repository holding the builder image. |
| `_BUILDER_TAG` | `latest` | Builder image tag. The full reference is composed per step as `${_AR_LOCATION}-docker.pkg.dev/${PROJECT_ID}/${_AR_REPO}/builder:${_BUILDER_TAG}`. |
| `_SSH_SECRET_NAME` | `gem-cluster-builder-ssh-key` | Secret Manager secret holding the private key. |
| `_SSH_SECRET_VERSION` | `latest` | Pin to a numeric version to roll back. |
