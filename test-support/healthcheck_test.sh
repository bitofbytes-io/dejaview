#!/usr/bin/env bash
# Exercises docker/healthcheck.sh with a fake wget: port precedence (PORT_FILE over
# PORT over 4600), strings.TrimSpace-compatible trimming of PORT_FILE (Unicode white
# space such as NBSP included), and the wget flags/URL. Also checks the Dockerfile
# wires the script in as the HEALTHCHECK.

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
script="${root}/docker/healthcheck.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

mkdir "${work}/bin"
cat > "${work}/bin/wget" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" > "${WGET_ARGS}"
exit "${WGET_EXIT:-0}"
EOF
chmod +x "${work}/bin/wget"

shells=(sh)
if command -v busybox >/dev/null 2>&1; then
  shells+=("busybox sh")
fi

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# probe <shell> [VAR=value ...] runs the script and leaves wget's argv in ${work}/args.
probe() {
  local shell="$1"
  shift
  rm -f "${work}/args"
  # shellcheck disable=SC2086
  # The fake is passed by absolute path via HEALTHCHECK_WGET: busybox sh runs its own
  # wget applet in preference to anything on PATH, so shadowing wget via PATH is not
  # hermetic. The script under test is otherwise run unmodified.
  env -i PATH="${PATH}" HEALTHCHECK_WGET="${work}/bin/wget" WGET_ARGS="${work}/args" "$@" ${shell} "${script}"
}

expect_url() {
  local name="$1" want="$2"
  shift 2
  local shell got
  for shell in "${shells[@]}"; do
    probe "${shell}" "$@" || fail "${name} (${shell}): probe exited non-zero"
    got="$(tail -n 1 "${work}/args")"
    [[ "${got}" == "${want}" ]] || fail "${name} (${shell}): url ${got@Q}, want ${want@Q}"
  done
}

port_file() {
  printf "$1" > "${work}/port"
  echo "${work}/port"
}

expect_url "default port" "http://127.0.0.1:4600/health"
expect_url "PORT override" "http://127.0.0.1:9000/health" PORT=9000
expect_url "PORT_FILE beats PORT" "http://127.0.0.1:8080/health" PORT=9000 PORT_FILE="$(port_file '8080')"
expect_url "empty PORT_FILE var falls back to PORT" "http://127.0.0.1:9000/health" PORT=9000 PORT_FILE=
expect_url "trailing newline" "http://127.0.0.1:8080/health" PORT_FILE="$(port_file '8080\n')"
expect_url "ASCII whitespace" "http://127.0.0.1:8080/health" PORT_FILE="$(port_file ' \t\r\n8080 \v\f\n')"
expect_url "NBSP" "http://127.0.0.1:8080/health" PORT_FILE="$(port_file '\302\240\302\2408080\302\240\n')"
expect_url "NEL and ideographic space" "http://127.0.0.1:8080/health" PORT_FILE="$(port_file '\302\205\343\200\2008080\343\200\200\302\205')"
expect_url "en/em/narrow/medium/line/para separators" "http://127.0.0.1:8080/health" \
  PORT_FILE="$(port_file '\342\200\200\342\200\212\342\200\250\342\200\2518080\342\200\257\342\201\237\342\200\201\341\232\200')"
expect_url "mixed ASCII and Unicode" "http://127.0.0.1:8080/health" PORT_FILE="$(port_file ' \302\240 \n8080\302\240\t \n')"
# Like strings.TrimSpace, interior white space is kept.
printf '80\302\24080' > "${work}/port"
want="$(printf 'http://127.0.0.1:80\302\24080/health')"
for shell in "${shells[@]}"; do
  probe "${shell}" PORT_FILE="${work}/port" || fail "interior NBSP (${shell}): non-zero exit"
  [[ "$(tail -n 1 "${work}/args")" == "${want}" ]] || fail "interior NBSP (${shell}): url changed"
done

# wget flags: ignore proxy env, quiet, discard body, and /health (not /ready).
probe sh PORT=9000
want_args=$'-q\n-Y\noff\n-O\n/dev/null\nhttp://127.0.0.1:9000/health'
[[ "$(cat "${work}/args")" == "${want_args}" ]] || fail "wget args: $(cat "${work}/args")"

# The probe's exit status follows wget's.
if probe sh WGET_EXIT=1; then
  fail "expected non-zero exit when wget fails"
fi

# An unreadable PORT_FILE fails the probe instead of probing a guessed port.
if probe sh PORT_FILE="${work}/missing"; then
  fail "expected non-zero exit for missing PORT_FILE"
fi
[[ ! -e "${work}/args" ]] || fail "wget ran despite missing PORT_FILE"

# The Dockerfile runs the script, installs it executable, and does not inline a probe.
grep -Eq '^HEALTHCHECK .*\\$' "${root}/Dockerfile" || fail "Dockerfile HEALTHCHECK missing"
grep -Fq 'CMD ["/app/healthcheck.sh"]' "${root}/Dockerfile" || fail "HEALTHCHECK does not run /app/healthcheck.sh"
grep -Eq '^COPY --chmod=0555 docker/healthcheck\.sh \./healthcheck\.sh$' "${root}/Dockerfile" || fail "Dockerfile does not install healthcheck.sh"
grep -Fq 'http://127.0.0.1:${port}/health"' "${script}" || fail "script must probe /health"
# Without the test seam the script must run plain wget from PATH.
grep -Fq 'exec "${HEALTHCHECK_WGET:-wget}" -q -Y off' "${script}" || fail "script must default to wget -q -Y off"
grep -Fq 'HEALTHCHECK_WGET' "${root}/Dockerfile" && fail "Dockerfile must not set HEALTHCHECK_WGET"
! grep -Fq '/ready' <(grep -v '^#' "${script}") || fail "script must not probe /ready"

echo "healthcheck tests passed"
