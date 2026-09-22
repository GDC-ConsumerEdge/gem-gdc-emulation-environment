# Code style

The GEM project consists of five languages: Terraform, Ansible, Python, Go and
Bash, plus a large amount of YAML and Markdown. Each of those has its own
community or language-standard conventions, and the GEM mostly follows those
conventions.

What this document records is the set of places where GEM departs from those
conventions, and the handful of rules that apply across all of them. If a
question is not answered here, the answer is the language's usual practice, and
the authority is the config file named in
[Where the rules live](#where-the-rules-live).

Where there is no clear style guidance, or conflicting information the
[Google Style Guides](https://google.github.io/styleguide/) is the overarching
reference.

## Automated Style Formatting

For most languages in this project, a formatter is defined and configured, which
can be initiated through `pre-commit`:

```bash
# Run all formatters and linters
pre-commit run --all-files

# Run the ruff formatter against a newly created Python file
pre-commit run ruff-format --files api/gem_api/new_file.py
```

The formatters rewrite files in place, so a failed run followed by a passing one
is the normal outcome, not an error. See
[CONTRIBUTING.md](../CONTRIBUTING.md#install-the-git-hooks) for installing the
hooks.

## Rules that apply everywhere

### Every file carries the Apache license header

This includes YAML, Bash, Terraform, Python, Go and Jinja templates. You do not
have to type it: the `addlicense` hook inserts a missing header, choosing the
comment syntax for the file type, so `pre-commit run --files <new files>` is
enough.

### Use `.yaml`, not `.yml`

Everything within this project uses a descriptive file extension (e.g. `.yaml`
not `.yml`). The exceptions are `.golangci.yml` and the files under `.github/`,
which follow the convention of the ecosystems that read them.

### Line length differs by language

There is no single number, because each formatter owns its own:

| Language      | Limit                          | Owned by                    |
| :------------ | :----------------------------- | :-------------------------- |
| Markdown      | 80, hard wrapped               | `.mdformat.toml`            |
| Python        | 88, reformatted by `ruff`      | `ruff` default              |
| YAML          | 160                            | `.yamlfmt`                  |
| Terraform, Go | Whatever the formatter decides | `terraform fmt` and `gofmt` |
| Bash          | Not enforced                   | Nothing                     |

### Suppress a lint at the line, never globally

Every configured linter runs with its full rule set. If a rule must be disabled,
do so at the source with a comment explaining why the rule was disabled.
Ideally, the linter violation would be resolved, but understanding there are
some situations where that's not practical.

- Terraform uses a `# tflint-ignore: <rule>` comment on the line above, with a
  plain comment above that explaining why.
- Python uses `# noqa: <rule>` at the end of the offending line.
- Ansible uses `.ansible-lint-ignore`, which takes a file path and a rule name.
  The `skip_list` in `.ansible-lint` is deliberately empty, and should stay that
  way.

### Commit messages are Conventional Commits

`release-please` builds the changelog from commit messages, so a non-conforming
subject fails presubmit. [CONTRIBUTING.md](../CONTRIBUTING.md#code-reviews) has
the detail.

## Terraform

Variable `description` fields are optional here, which departs from the usual
advice to document every input. The `tflint` recommended preset does not require
them, and most GEM variables are named well enough that a description would only
restate the name. Add one when the name does not tell the whole story, as
`hardware_variant` does by pointing the reader at the file listing the variants.

Validation error messages are written for whoever hits them, not for whoever
wrote the rule. State the constraint and the reason, rather than naming the
failed condition. The cluster name length check is the model.

Resource files are named for what they contain, with hyphens, as in
`cluster-nodes.tf` and `hardware-variants.tf`, alongside the conventional
`main.tf`, `variables.tf`, `outputs.tf` and `backend.tf`.
[skills/add-terraform-module.md](../skills/add-terraform-module.md) covers the
full module layout, including why `backend.tf` is an empty stub.

## Ansible

Module names are always fully qualified, as in `ansible.builtin.template` rather
than `template`. `ansible-lint` enforces this, and it runs with an empty
`skip_list`, so every rule in its default profile applies. That includes rules
many projects disable, notably the requirement that every task has a `name`, and
that the name starts with a capital letter.

Shared values belong in `ansible/group_vars/all.yaml` rather than inline in a
task. Roles are `lower_snake_case`, matching the directories already there, and
[skills/add-ansible-role.md](../skills/add-ansible-role.md) covers the layout.

Jinja templates are excluded from YAML linting and formatting, since a `.j2`
file is not valid YAML until it is rendered. This is why template files take a
`{# ... #}` license header rather than a `#` one.

## Python

The API targets Python 3.13 and above, so write modern syntax: `str | None`
rather than `Optional[str]`, and built-in generics rather than the `typing`
equivalents.

Linting and formatting are `ruff` at its defaults, with two project settings in
`api/pyproject.toml`. `gem_api` is declared first-party so import sorting groups
it correctly, and `B008` is ignored so that FastAPI's `Depends()` in an argument
default does not get flagged.

`pytest-asyncio` runs in `auto` mode, which departs from its default. Async test
functions need no `@pytest.mark.asyncio` decorator.

The API is an application rather than a library, declared with
`[tool.uv] package = false`. `uv` does not install `gem_api` into the
environment, so `uv` commands have to run from the `api/` directory.

Docstrings explain why the code is designed the way it is, not what it does. A
docstring that restates the signature is noise.

## Go

There is one Go module, `operators/gem-network-operator`, tied into the
repository by `go.work` so that root-level tooling and IDEs resolve it without
per-tool configuration.

Its module path is under `github.com/GoogleCloudPlatform/gem`, which is not the
URL this repository lives at. The path is internal and nothing fetches it, but
`goimports` is configured with a matching `local-prefixes` value so that project
imports sort into their own group. If you ever change one, change the other.

`golangci-lint` runs with `default: none` and an explicit list of linters, so
adding a linter is a deliberate edit to `.golangci.yml` rather than a side
effect of a tool upgrade. The list is close to the standard set, with
`whitespace` added.

## Bash

Every script starts with `#!/bin/bash` and `set -euo pipefail`, in that order,
below the license header. Scripts target bash specifically, not POSIX `sh`.

Indentation is two spaces in most of the tree, but nothing enforces it. This
project endeavors to follow the
[Google Shell Style Guide](https://google.github.io/styleguide/shellguide.html),
which should be used as a reference for any Bash style questions. `shellcheck`
is used to validate script correctness rather than formatting

`shellcheck` runs from your `PATH` rather than from a container, which is a
deliberate choice to avoid a Docker dependency for a single binary. You need it
installed locally.

## YAML

`yamlfmt` runs in `basic` mode with `retain_line_breaks` enabled, so the blank
lines you use to group related keys survive formatting. Folded scalars are
scanned as literal, which keeps embedded shell and Jinja intact.

Ansible role templates and the operator CRDs are excluded from both YAML linting
and formatting. Templates are not valid YAML before rendering, and the CRDs are
generated.

## Markdown

`mdformat` wraps at 80 columns and otherwise follows the
[Google Markdown style guide](https://google.github.io/styleguide/docguide/style.html).

- Ordered lists are renumbered consecutively, so the common practice of writing
  every item as `1.` gets rewritten to `1.`, `2.`, `3.`.

- GitHub alerts are kept on separate lines because the `gfm_alerts` extension is
  installed. Without this, `mdformat` collapses a `> [!NOTE]` and its body onto
  one line, which stops GitHub rendering it.

`CHANGELOG.md` is generated by `release-please` and is excluded from formatting,
since reformatting it would produce a conflict on every release.

If you are using an AI Agent to generate or revise documentation, please ensure
you reference [the technical writer skill](../skills/technical-writer.md), which
contains detail on the documentation style for this project.

## Where the rules live

| Language  | Config                                                                                   |
| :-------- | :--------------------------------------------------------------------------------------- |
| All       | [.pre-commit-config.yaml](../.pre-commit-config.yaml)                                    |
| Terraform | [terraform/.tflint.hcl](../terraform/.tflint.hcl)                                        |
| Ansible   | [.ansible-lint](../.ansible-lint), `.ansible-lint-ignore`, [ansible.cfg](../ansible.cfg) |
| Python    | [api/pyproject.toml](../api/pyproject.toml)                                              |
| Go        | [.golangci.yml](../.golangci.yml), [go.work](../go.work)                                 |
| YAML      | [.yamlfmt](../.yamlfmt)                                                                  |
| Markdown  | [.mdformat.toml](../.mdformat.toml)                                                      |
