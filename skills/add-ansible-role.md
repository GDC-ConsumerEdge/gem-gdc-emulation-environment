# Add an Ansible role

GEM configures hosts with classic Ansible roles under `ansible/roles/<name>/`.
Match the existing roles (`vxlan`, `topolvm`, `gatekeeper`, …) rather than
inventing a new layout.

## Layout

Create only the subdirectories the role actually uses:

```
ansible/roles/<role_name>/
  tasks/main.yaml         # required — the role entry point
  templates/              # optional — *.j2 rendered onto hosts
  handlers/main.yaml      # optional — restart/notify handlers
  files/                  # optional — static files copied verbatim
```

Role names are `lower_snake_case` to match the directories already in use.

## Conventions

- Every YAML and Bash file needs the Apache license header (see below). Jinja
  templates (`.j2`) use a `{# … #}` header instead of `#`. You do not have to
  type these by hand — the `addlicense` pre-commit hook inserts missing headers;
  run `pre-commit run --files <new files>` and they are added.
- Use fully-qualified module names (`ansible.builtin.template`, not `template`);
  ansible-lint enforces this.
- Put shared variables in `ansible/group_vars/all.yaml`, not hardcoded in tasks.
- Common vars available to roles include `gem_user`, `gem_home`, and
  `cluster_name`; reuse them.

## Wire the role into a play

A role does nothing until a play references it. Add the role to the relevant
playbook (usually `ansible/create-cluster.yaml`) under the `roles:` list of the
play whose `hosts:` matches where it should run — e.g. `workstation`,
`cluster_nodes`, or `gdc_nodes`. Order matters; roles run top to bottom.

```yaml
- name: Deploy Google Distributed Cloud Hybrid Cluster
  hosts: workstation
  become: true
  roles:
    - gdc_deploy
    - topolvm
    - <role_name>   # add in the correct position
```

## Validate

Follow [validate.md](validate.md) — `pre-commit run --files <new files>` for
ansible-lint, then `./scripts/run-unit-tests.sh`. Do not run a live playbook to
test the role.

## Apache license header (YAML / Bash)

```
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
```
