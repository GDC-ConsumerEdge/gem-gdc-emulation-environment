#!/bin/bash
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

# Open a pull request whose description lists every commit on the branch as a
# release note entry. Pull requests are squash merged, so without this only the
# PR title reaches the changelog. release-please replaces the squash commit
# message with the contents of the override block, and reads it from the PR
# description each time it runs, so entries can be deleted in the GitHub UI
# before or after merging.

set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: new-pr.sh --title "<type>(<scope>): <summary>" [options] [-- <gh args>]

Prints the PR description and exits unless --create is given.

Required:
  --title <title>          Conventional Commits PR title. Also becomes the
                           first release note entry.

Options:
  --base <branch>          Branch to merge into (default: main)
  --release-as <x.y.z>     Force the next release version. Added inside the
                           override block, the only place it takes effect.
  --create                 Push the branch if needed and open the PR.
  --dry-run                Print the PR description and exit. Pushes nothing.
                           This is the default; the flag overrides --create.
  -h, --help               Display this help message

Anything after -- is passed to `gh pr create`, for example:
  new-pr.sh --title "docs: rewrite setup guide" --create -- --draft --reviewer octocat
USAGE
}

title=""
base="main"
release_as=""
dry_run=false
create=false
gh_args=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --title)
      title="${2:?--title requires a value}"
      shift 2
      ;;
    --base)
      base="${2:?--base requires a value}"
      shift 2
      ;;
    --release-as)
      release_as="${2:?--release-as requires a value}"
      shift 2
      ;;
    --create)
      create=true
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    --)
      shift
      gh_args=("$@")
      break
      ;;
    *)
      echo "❌ Unknown option: $1" >&2
      echo "Run '$0 --help' for usage." >&2
      exit 1
      ;;
  esac
done

if [[ -z "$title" ]]; then
  echo "❌ --title is required." >&2
  echo "Run '$0 --help' for usage." >&2
  exit 1
fi

# Fail here rather than in the lint-pr-title check after the PR is open.
if ! [[ "$title" =~ ^[a-z]+(\([^\)]+\))?!?:\ .+ ]]; then
  echo "❌ Title is not a Conventional Commits title: $title" >&2
  echo "   Expected <type>(<scope>): <summary>, for example 'fix(api): handle empty body'" >&2
  exit 1
fi

if [[ -n "$release_as" ]] && ! [[ "$release_as" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "❌ --release-as must be a version like 1.0.0, got: $release_as" >&2
  exit 1
fi

for cmd in git gh; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "❌ Missing required tool: $cmd" >&2
    exit 1
  fi
done

branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" == "$base" || "$branch" == "HEAD" ]]; then
  echo "❌ Check out a feature branch first (currently on '$branch')." >&2
  exit 1
fi

git fetch --quiet origin "$base"

# Oldest first, so the list reads in the order the work was done. Merge commits
# from syncing with the base branch are not release notes.
mapfile -t subjects < <(git log --reverse --no-merges --format='%s' "origin/${base}..HEAD")

if [[ ${#subjects[@]} -eq 0 ]]; then
  echo "❌ No commits on '$branch' that are not already on origin/${base}." >&2
  exit 1
fi

body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT

# The comment must not contain the literal marker text: release-please splits
# the description on the first occurrence of it.
{
  echo "<!-- Describe the change for reviewers here. -->"
  echo
  echo "<!--"
  echo "Release notes: each line in the block below becomes its own entry in"
  echo "CHANGELOG.md and the GitHub release, replacing the squash commit message."
  echo "Delete any entry you do not want published. chore: and style: entries are"
  echo "hidden automatically. Keep a blank line between entries."
  echo "-->"
  echo "BEGIN_COMMIT_OVERRIDE"
  echo "$title"
  for subject in "${subjects[@]}"; do
    # The PR title is already the first entry.
    [[ "$subject" == "$title" ]] && continue
    echo
    echo "$subject"
  done
  if [[ -n "$release_as" ]]; then
    echo
    echo "Release-As: $release_as"
  fi
  echo "END_COMMIT_OVERRIDE"
} >"$body_file"

if [[ "$create" != true || "$dry_run" == true ]]; then
  echo "Title: $title"
  echo "Base:  $base <- $branch (${#subjects[@]} commits)"
  echo
  cat "$body_file"
  echo
  echo "ℹ️  Dry run: nothing was pushed. Re-run with --create to open the PR."
  exit 0
fi

# gh pr create will not open a PR for a branch that is not on the remote.
if ! git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
  git push --set-upstream origin HEAD
fi

gh pr create \
  --base "$base" \
  --title "$title" \
  --body-file "$body_file" \
  ${gh_args[@]+"${gh_args[@]}"}
