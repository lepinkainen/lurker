#!/bin/sh
# Own a disposable Ergo instance for the command (or a manual session).
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ergo_id=
cleanup() {
  if [ -n "$ergo_id" ]; then
    docker rm -f "$ergo_id" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

ergo_id=$(docker run -d --rm -p 127.0.0.1:16667:6667 \
  -v "$repo_root/testdata/ergo/ircd.yaml:/ircd/ircd.yaml:ro" \
  ghcr.io/ergochat/ergo:stable)
attempt=0
until nc -z 127.0.0.1 16667; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 100 ]; then
    docker logs "$ergo_id" >&2 || true
    echo "Ergo did not start listening on :16667" >&2
    exit 1
  fi
  sleep 0.2
done
export ERGO_ADDR=127.0.0.1:16667
if [ "$#" -eq 0 ]; then
  echo "Ergo ready at $ERGO_ADDR; Ctrl-C stops this instance"
  docker logs -f "$ergo_id"
else
  "$@"
fi
