#!/bin/sh
# Container HEALTHCHECK probe: GET /health on the port the app listens on.
# /health only reports that the process answers; /ready also pings the database,
# which would mark the container unhealthy (and restart it) on a database outage.
#
# Port precedence matches config.Load: PORT_FILE (trimmed) wins over PORT, default 4600.
# getEnv trims PORT_FILE with strings.TrimSpace, which strips leading/trailing Unicode
# white space (including NBSP) and leaves interior characters alone, so do the same here.
set -u

nl='
'
# Every rune strings.TrimSpace treats as white space, as UTF-8 bytes.
set -- ' ' "$(printf '\t')" "$nl" "$(printf '\v')" "$(printf '\f')" "$(printf '\r')" \
  "$(printf '\302\205')" "$(printf '\302\240')" "$(printf '\341\232\200')" \
  "$(printf '\342\200\200')" "$(printf '\342\200\201')" "$(printf '\342\200\202')" \
  "$(printf '\342\200\203')" "$(printf '\342\200\204')" "$(printf '\342\200\205')" \
  "$(printf '\342\200\206')" "$(printf '\342\200\207')" "$(printf '\342\200\210')" \
  "$(printf '\342\200\211')" "$(printf '\342\200\212')" "$(printf '\342\200\250')" \
  "$(printf '\342\200\251')" "$(printf '\342\200\257')" "$(printf '\342\201\237')" \
  "$(printf '\343\200\200')"

port="${PORT:-4600}"
if [ -n "${PORT_FILE:-}" ]; then
  port="$(cat -- "$PORT_FILE")" || exit 1
  changed=1
  while [ "$changed" = 1 ]; do
    changed=0
    for ws in "$@"; do
      case "$port" in
        "$ws"*) port="${port#"$ws"}"; changed=1 ;;
      esac
      case "$port" in
        *"$ws") port="${port%"$ws"}"; changed=1 ;;
      esac
    done
  done
fi

# -Y off: ignore proxy env, the probe targets loopback.
# HEALTHCHECK_WGET is a test seam: busybox sh prefers its built-in wget applet over
# PATH, so tests cannot shadow wget via PATH. It is never set in the image.
exec "${HEALTHCHECK_WGET:-wget}" -q -Y off -O /dev/null "http://127.0.0.1:${port}/health"
