# Managing GEM Clusters with Cloud Build

Building a GEM cluster by hand requires installing Terraform and Ansible
locally, then holding a terminal open for the length of a cluster build. GEM
ships two Cloud Build pipelines that do the same work in GCP instead, which
makes builds repeatable and visible to everyone with access to your GCP project.

| Pipeline         | Config                                        | Purpose                                   |
| :--------------- | :-------------------------------------------- | :---------------------------------------- |
| Cluster build    | `cloudbuild/cluster-build.cloudbuild.yaml`    | Provisions and configures one GEM cluster |
| Cluster teardown | `cloudbuild/cluster-teardown.cloudbuild.yaml` | Resets and destroys one GEM cluster       |

Both pipelines run the same Terraform modules and Ansible playbooks you would
run yourself, but neither creates nor manages the shared infrastructure. The GCP
project foundation and the admin workstation have to exist before you submit
your first build. The edge router is optional, and a build works without one.

## One-time setup

You'll need `project-setup.sh` to have run successfully, the foundation and
admin workstation deployed (see [Project Setup](project-setup.md) and
[Admin Workstation](admin-workstation.md)), Terraform 1.12.2 or later and
Ansible installed locally, and a `gcloud` login that can impersonate
`${PROVISIONING_SA_EMAIL}`. The impersonation is managed through
`project-setup.sh`, which grants your account
`roles/iam.serviceAccountTokenCreator` on the provisioning service account.

> [!NOTE]
> Submitting a build also requires `roles/iam.serviceAccountUser` on
> `gem-cluster-builder@`, because the builds run as that service account.
> Nothing in this repository grants it, so a project Owner gets it implicitly
> and everyone else needs it granted. Without it, `gcloud builds submit` fails
> with a `PERMISSION_DENIED` on `iam.serviceAccounts.actAs`.

### 1. Apply the Cloud Build Terraform module

[`cloudbuild/setup.sh`](../cloudbuild/setup.sh) manages a Terraform run to setup
the required Cloud Build resources. It takes no flags, and reads three required
environment variables:

```bash
export PROJECT_ID=your-gcp-project-id
export TF_STATE_BUCKET=gem-${PROJECT_ID}-tfstate
export PROVISIONING_SA_EMAIL=tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com

cd ${REPO_ROOT}/cloudbuild
./setup.sh
```

`REPO_ROOT` is optional and defaults to the repository the script is running
from. The snippets throughout this page use it, so export it if you want to
paste them from anywhere.

The script initializes the backend at prefix `cloudbuild/state`, applies
[terraform/cloudbuild](../terraform/cloudbuild), then prints to your terminal
what you need for the rest of setup.

The GEM Cloud Build Terraform module creates a `gem-cluster-builder` service
account, an Artifact Registry repository and an empty Secret Manager secret. The
builder account can't provision anything itself. It impersonates your
provisioning account to do that, and otherwise holds only what it needs to read
and write Terraform state, push and pull the builder image, tunnel through IAP
and run preflight lookups. Those roles are in
[`iam.tf`](../terraform/cloudbuild/iam.tf), except the grant that lets it read
the SSH secret, which is in [`secrets.tf`](../terraform/cloudbuild/secrets.tf).

> [!IMPORTANT]
> `setup.sh` passes only `project_id` and `provisioning_sa_email` on the command
> line. The module's `region`, `zone` and `ar_location` variables have no
> defaults and are read from `terraform/cloudbuild/terraform.tfvars`, which
> `project-setup.sh` generates. If that file is missing, `terraform apply`
> prompts you for all three, and fails outright when there is no terminal to
> prompt on. Re-run `project-setup.sh` first.

### 2. Publish the workstation SSH key

The build pipelines reach the admin workstation over SSH, using a private key
they read from Secret Manager at build time. The GEM Cloud Build module creates
an empty secret, and the admin workstation Ansible role uploads a copy of its
SSH private key into it. Ordering matters here, so apply the GEM Cloud Build
module first, then re-run the admin workstation Ansible playbook.

```bash
cd ${REPO_ROOT}/ansible
CLUSTER_NAME=none ansible-playbook admin-workstation.yaml
```

`CLUSTER_NAME=none` is required only because `ansible/inventory.sh` expects the
variable to be set. The workstation playbook itself does not use it.

`setup.sh` checks whether the secret has a version and prints an ACTION REQUIRED
block naming this playbook when it does not. It is advisory: the script still
exits successfully, so it is on you to notice.

### 3. Build the builder image

Each pipeline step runs in a custom container image which bundles Terraform,
Ansible and the Google Cloud CLI. You'll build this image once during setup, and
again whenever the [Dockerfile](../cloudbuild/builder/Dockerfile) changes.

```bash
# AR_LOCATION was printed by setup.sh in the previous step
export AR_LOCATION=$(terraform -chdir=${REPO_ROOT}/terraform/cloudbuild \
  output -raw artifact_registry_location)

gcloud builds submit \
  --config=${REPO_ROOT}/cloudbuild/builder/cloudbuild.yaml \
  --substitutions=_AR_LOCATION=${AR_LOCATION} \
  --service-account=projects/${PROJECT_ID}/serviceAccounts/gem-cluster-builder@${PROJECT_ID}.iam.gserviceaccount.com \
  ${REPO_ROOT}/cloudbuild/builder
```

Unlike the cluster pipelines, `builder/cloudbuild.yaml` declares no service
account of its own, so pass `--service-account` explicitly. Without it the build
falls back to the project's default Cloud Build account, which does not hold the
Artifact Registry grants this module creates. The build context must be
`cloudbuild/builder`.

The image is tagged `latest` unless you set `_IMAGE_TAG` when building the build
image. If you do change the image tag you will need to pass the same tag value
as `_BUILDER_TAG` when submitting a cluster build, otherwise the build pipeline
won't find the image.

## Building a cluster

With setup out of the way, a cluster build is one command. `GEM_GCP_ZONE` comes
from your [project setup](project-setup.md) environment; if it is unset the
build fails in its `setup` step on an empty zone.

```bash
gcloud builds submit \
  --config=${REPO_ROOT}/cloudbuild/cluster-build.cloudbuild.yaml \
  --substitutions=_CLUSTER_NAME=gem-cluster-1,_AR_LOCATION=${AR_LOCATION},_GEM_GCP_ZONE=${GEM_GCP_ZONE} \
  ${REPO_ROOT}
```

> [!NOTE]
> Because `*.tfvars` is not included with a build request, the pipelines pass
> every Terraform input the module requires. Editing a `terraform.tfvars` file
> has no effect on a Cloud Build run. You will need to define your cluster
> parameters through Cloud Build substitutions as seen in the example above.
> Anything you do not pass falls back to the module default rather than to your
> local tfvars.

## Destroying a cluster

A cluster teardown is similar, pointed at the other Cloud Build pipeline:

```bash
gcloud builds submit \
  --config=${REPO_ROOT}/cloudbuild/cluster-teardown.cloudbuild.yaml \
  --substitutions=_CLUSTER_NAME=gem-cluster-1,_AR_LOCATION=${AR_LOCATION},_GEM_GCP_ZONE=${GEM_GCP_ZONE} \
  ${REPO_ROOT}
```

### Configuration options

Substitutions are how you configure a build. These are the ones you're likely to
set:

| Substitution                                 | Default                      | Notes                                                                                                         |
| :------------------------------------------- | :--------------------------- | :------------------------------------------------------------------------------------------------------------ |
| `_CLUSTER_NAME`                              | `gem-cluster-1`              | Names the cluster, node VMs, and the fleet membership                                                         |
| `_GEM_GCP_ZONE`                              | Required                     | For example, `us-central1-a`. The region is derived by dropping the last segment (`us-central1`).             |
| `_AR_LOCATION`                               | Required                     | Artifact Registry location where your builder images reside. For example, `us-central1`                       |
| `_EMULATE_GDC_VERSION`                       | Ansible default              | Which GDC version to emulate. If left empty, defaults to the latest available version                         |
| `_HARDWARE_VARIANT`                          | `g2-small-64gb`              | Which GDC hardware configuration to emulate                                                                   |
| `_DESTROY_ON_FAILURE`                        | `true`                       | Set to `false` to keep a failed cluster build around for inspection                                           |
| `_SSH_SECRET_VERSION`                        | `latest`                     | Pin to a numeric version to roll back a rotated key                                                           |
| `_TF_STATE_BUCKET`, `_PROVISIONING_SA_EMAIL` | Derived from `${PROJECT_ID}` | Resolved at runtime to `gem-${PROJECT_ID}-tfstate` and `tf-provisioner@${PROJECT_ID}.iam.gserviceaccount.com` |

> [!WARNING]
> Overriding `_TF_STATE_BUCKET` will break the build. The Terraform steps honour
> it, but `ansible/inventory.sh` hardcodes `gem-${PROJECT_ID}-tfstate`, so the
> `ansible-create-cluster` step would build its inventory from a different
> bucket and find no hosts. A non-default state bucket is not supported end to
> end.

Teardown accepts the same substitutions, minus the three that only mean
something when you're creating a cluster: `_HARDWARE_VARIANT`,
`_EMULATE_GDC_VERSION` and `_DESTROY_ON_FAILURE`. The rest rarely need changing
and are declared at the top of each config. One of them is worth knowing about:
if you change the Artifact Registry repository away from `gem`, you have to pass
`_AR_REPO` to both the builder image build and every cluster build.

### Monitoring a build

`gcloud builds submit` streams output to your terminal. If you detached, or want
to catch up on an earlier run:

```bash
# Follow a build already in progress
gcloud builds log <BUILD_ID> --stream

# Find every build for one cluster
gcloud builds list --filter="tags='gem-cluster-build' AND tags='gem-cluster-1'"
```

Every build is tagged `gem-cluster-build` or `gem-cluster-delete`, along with
the cluster name and the emulated GDC version. Builds also carry a branch tag,
but only when they come from a trigger. The `gcloud builds submit` workflow
above leaves it empty.

## How a build runs

Steps run sequentially, and a build is expected to take somewhere around 30
minutes to complete.

| Step                     | What it does                                                                                     |
| :----------------------- | :----------------------------------------------------------------------------------------------- |
| `setup`                  | Validates the zone, derives defaults, sets up the build environment                              |
| `preflight`              | Validates cluster name length, and ensures that a cluster with this name does not already exist  |
| `terraform-apply`        | Provisions the node VMs                                                                          |
| `ansible-create-cluster` | Runs `create-cluster.yaml`, which orchestrates a `bmctl create cluster` on the admin workstation |
| `teardown-on-failure`    | Destroys the cluster if an earlier stage failed and `_DESTROY_ON_FAILURE` is `true`              |
| `status`                 | Fails the build if any stage recorded a failure                                                  |

Teardown reverses the order, running `ansible-cleanup` before
`terraform-destroy`. It has no preflight, no conditional teardown and no status
step.

The provisioning steps do not fail the build directly. They are marked
`allowFailure`, and on error they write the name of the stage that failed to
`/workspace/state/failed-stage`. Later steps read that file, which is how
`teardown-on-failure` knows to clean up and how `status` decides the build's
exit code. This is what lets a failed build still run its own teardown, and it
is why build logs talk about a `failed-stage` rather than a step exiting
non-zero.

A cluster build times out at 5400 seconds, with the `ansible-create-cluster`
step separately capped at 3600. Teardown times out at 1800. A build killed by a
timeout never reaches `teardown-on-failure`, so nothing is cleaned up for you.

### Preflight checks

[`preflight.sh`](../cloudbuild/steps/preflight.sh) attempts to catch the most
common errors before a build starts, and will prevent a build from starting for
the following reasons:

- `len(cluster_name) + len(zone) + len(project_id) + 15 > 63`, which would push
  node FQDNs past the Kubernetes 63-character label limit.
- Any of `<cluster>-1`, `<cluster>-2` or `<cluster>-3` already exists as a VM.
- A fleet membership named `<cluster>` already exists.

> [!NOTE]
> `terraform/cluster` separately rejects any cluster name longer than 26
> characters. Preflight does not check for this, so in a project with a short
> name and zone an over-long name passes preflight and then fails in
> `terraform-apply`.

## Failure modes

| Symptom                                   | Cause                                                                   | Resolution                                                                                                                            |
| :---------------------------------------- | :---------------------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------------------ |
| Fails at `setup` with a zone error        | `_GEM_GCP_ZONE` empty or malformed                                      | Pass a valid zone                                                                                                                     |
| Fails pulling the step image              | `_AR_LOCATION` empty, `_BUILDER_TAG` mismatched, or no image            | Check the Artifact Registry location and rebuild the image                                                                            |
| Fails before `setup` on secret resolution | The SSH secret has no versions                                          | Re-run `admin-workstation.yaml`                                                                                                       |
| Preflight rejects the build               | Leftover VMs, leftover fleet membership, or a long cluster name         | Run the printed cleanup commands, or shorten the cluster name                                                                         |
| `failed-stage=tf-init`                    | Backend permissions, a bad state bucket, or a provider download failure | Check the bucket name and the impersonation grant                                                                                     |
| `failed-stage=tf-apply`                   | Quota, permissions, a Terraform error, or a held state lock             | Read the build log. Teardown runs unless `_DESTROY_ON_FAILURE=false`. `terraform force-unlock` if a previous run was killed mid-apply |
| `failed-stage=ansible-create-cluster`     | A `bmctl` or configuration failure                                      | Read the build log, then the `bmctl` log on the workstation                                                                           |
| Build hits its timeout                    | A slow or wedged cluster creation                                       | Clean up manually, as described below                                                                                                 |

After any build failure that was not automatically cleaned up, it's worth
checking what cluster artifacts persist:

```bash
gcloud compute instances list --filter="name~'^${CLUSTER_NAME}-'"
gcloud container fleet memberships list --filter="name~'${CLUSTER_NAME}'"
gcloud storage ls gs://${TF_STATE_BUCKET}/clusters/${CLUSTER_NAME}/state/
gcloud storage ls gs://gem-${PROJECT_ID}-overlay-sync/
```

Submitting a cluster teardown build is the most reliable way to clean up
orphaned resources. If the teardown also fails, run the equivalent by hand:

```bash
cd ${REPO_ROOT}
CLUSTER_NAME=${CLUSTER_NAME} ansible-playbook ansible/cleanup.yaml \
  -e cluster_name=${CLUSTER_NAME}

# terraform/cluster/backend.tf is an empty gcs block, so init has to supply it
terraform -chdir=terraform/cluster init -reconfigure \
  -backend-config="bucket=${TF_STATE_BUCKET}" \
  -backend-config="prefix=clusters/${CLUSTER_NAME}/state" \
  -backend-config="impersonate_service_account=${PROVISIONING_SA_EMAIL}"

terraform -chdir=terraform/cluster destroy -var=cluster_name=${CLUSTER_NAME}
```

## A note on the SSH secret

The private key in `gem-cluster-builder-ssh-key` grants `gem` access to the
admin workstation and every cluster node, which amounts to cluster-admin on
every GEM cluster in the project. Restrict `roles/secretmanager.secretAccessor`
on it accordingly, and be equally careful about who can submit builds as
`gem-cluster-builder@`. To rotate the key, add a new secret version and pin
`_SSH_SECRET_VERSION` while the transition is in flight.

## Concurrency with the REST API

Cloud Build cluster builds and the [GEM REST API](gem-api.md) both utilize the
same Terraform state, so running both against the same cluster puts them in
contention for a single state lock. You can use either the REST API or the Cloud
Build pipelines, but not simultaneously.

## Related documentation

- [Project Setup](project-setup.md) for `project-setup.sh` and `GEM_AR_LOCATION`
- [Admin Workstation](admin-workstation.md) for the SSH key handshake
- [GEM REST API](gem-api.md) for the alternative orchestration path
