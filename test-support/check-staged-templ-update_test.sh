#!/usr/bin/env bash
# Exercises the staged templ-update allowlist against a malicious rename and
# against non-regular index modes.

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
checker="${root}/test-support/check-staged-templ-update.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

cd "${work}"
git init -q
git config user.email "templ-update-check@example.com"
git config user.name "templ-update-check"
mkdir -p .github/workflows internal/ui
printf 'name: CI\n' > .github/workflows/ci.yml
printf 'module example\n\ngo 1.26.10\n' > go.mod
printf 'sum v1\n' > go.sum
printf 'package ui\n' > internal/ui/login_templ.go
git add .
git commit -q -m init

expect_reject() {
  local name="$1"
  if bash "${checker}"; then
    echo "expected rejection: ${name}" >&2
    exit 1
  fi
  git reset -q --hard
  git clean -q -fd
}

expect_allow() {
  local name="$1"
  if ! bash "${checker}"; then
    echo "expected allow: ${name}" >&2
    exit 1
  fi
  git reset -q --hard
  git clean -q -fd
}

# Renaming the workflow to a *_templ.go name is invisible to git apply --numstat.
git mv .github/workflows/ci.yml ok_templ.go
git diff --cached --binary > "${work}/rename.patch"
git reset -q --hard
git apply --index --binary "${work}/rename.patch"
expect_reject "rename .github/workflows/ci.yml to ok_templ.go"

ln -sfn /tmp go.mod
git add -- go.mod
expect_reject "replace go.mod with a symlink"

ln -sfn /tmp ok_templ.go
git add -- ok_templ.go
expect_reject "add symlink named ok_templ.go"

git update-index --add --cacheinfo "160000,$(git rev-parse HEAD),go.mod"
expect_reject "replace go.mod with a gitlink"

printf 'sum v2\n' > go.sum
printf 'package ui\n\nfunc Login() {}\n' > internal/ui/login_templ.go
git add -- go.sum internal/ui/login_templ.go
expect_allow "regular go.sum and *_templ.go modifications"

echo "staged templ update allowlist passed"
