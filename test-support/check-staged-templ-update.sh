#!/usr/bin/env bash
# Reject a staged diff unless every entry is a regular-file add, modification,
# or deletion of go.mod, go.sum, or *_templ.go.
#
# git apply --numstat is not enough: a rename is reported only under its
# destination, and a symlink keeps the same path. Inspect the index after
# apply, with rename detection off, and reject non-regular modes.

set -euo pipefail

allowed_path() {
  case "$1" in
    go.mod|go.sum|*_templ.go) return 0 ;;
    *) return 1 ;;
  esac
}

regular_mode() {
  case "$1" in
    100644|100755) return 0 ;;
    *) return 1 ;;
  esac
}

name_status="$(git diff --cached --name-status --no-renames)"
raw="$(git diff --cached --raw --no-renames)"

name_count=0
while IFS=$'\t' read -r status path extra; do
  if [ -z "${status}" ]; then
    continue
  fi
  if [ -n "${extra:-}" ] || [ -z "${path}" ]; then
    echo "Staged diff has a rename, copy, or unexpected path: ${status} ${path} ${extra:-}" >&2
    exit 1
  fi
  case "${status}" in
    A|M|D) ;;
    *)
      echo "Staged diff status ${status} is not an add, modification, or deletion (${path})" >&2
      exit 1
      ;;
  esac
  if ! allowed_path "${path}"; then
    echo "Staged diff touches ${path}" >&2
    exit 1
  fi
  name_count=$((name_count + 1))
done <<< "${name_status}"

if [ "${name_count}" -eq 0 ]; then
  echo "Staged diff is empty" >&2
  exit 1
fi

raw_count=0
while IFS=$'\t' read -r meta path extra; do
  if [ -z "${meta}" ]; then
    continue
  fi
  if [ -n "${extra:-}" ] || [ -z "${path}" ]; then
    echo "Staged diff has a rename, copy, or unexpected path: ${meta} ${path} ${extra:-}" >&2
    exit 1
  fi
  IFS=' ' read -r oldmode newmode _oldsha _newsha status <<< "${meta#:}"
  case "${status}" in
    A|M|D) ;;
    *)
      echo "Staged diff status ${status} is not an add, modification, or deletion (${path})" >&2
      exit 1
      ;;
  esac
  if ! allowed_path "${path}"; then
    echo "Staged diff touches ${path}" >&2
    exit 1
  fi
  case "${oldmode}:${newmode}" in
    *120000*|*160000*)
      echo "Staged diff mode ${oldmode} -> ${newmode} is not a regular file (${path})" >&2
      exit 1
      ;;
  esac
  case "${status}" in
    A)
      if [ "${oldmode}" != "000000" ] || ! regular_mode "${newmode}"; then
        echo "Staged addition of ${path} is not a regular file (${oldmode} -> ${newmode})" >&2
        exit 1
      fi
      ;;
    D)
      if [ "${newmode}" != "000000" ] || ! regular_mode "${oldmode}"; then
        echo "Staged deletion of ${path} is not a regular file (${oldmode} -> ${newmode})" >&2
        exit 1
      fi
      ;;
    M)
      if ! regular_mode "${oldmode}" || ! regular_mode "${newmode}"; then
        echo "Staged modification of ${path} is not a regular file (${oldmode} -> ${newmode})" >&2
        exit 1
      fi
      ;;
  esac
  raw_count=$((raw_count + 1))
done <<< "${raw}"

if [ "${raw_count}" -ne "${name_count}" ]; then
  echo "Staged name-status and raw diffs disagree (${name_count} vs ${raw_count})" >&2
  exit 1
fi
