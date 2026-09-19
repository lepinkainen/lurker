#!/bin/sh
set -eu

test_root=$(mktemp -d)
fake_pid=
backend_pid=

cleanup() {
  if [ -n "$backend_pid" ]; then
    kill "$backend_pid" 2>/dev/null || true
  fi
  if [ -n "$fake_pid" ]; then
    kill "$fake_pid" 2>/dev/null || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

cat >"$test_root/config.yaml" <<'EOF'
networks:
  - network: IRCUITest
    nick: lurker
    channels:
      - "#timeline-scroll"
    servers:
      - host: 127.0.0.1
        port: 16667
        tls: false
EOF

go build -o "$test_root/fakeircd" ./cmd/fakeircd
go build -o "$test_root/lurker" .

"$test_root/fakeircd" -addr 127.0.0.1:16667 -ctl 127.0.0.1:16668 \
  >"$test_root/fakeircd.log" 2>&1 &
fake_pid=$!

DATA_DIR="$test_root/data" CONFIG_PATH="$test_root/config.yaml" ADDR=127.0.0.1:18081 \
  "$test_root/lurker" >"$test_root/backend.log" 2>&1 &
backend_pid=$!

ready=false
attempt=0
while [ "$attempt" -lt 100 ]; do
  if curl -fsS http://127.0.0.1:18081/whoami >/dev/null 2>&1 \
    && rg -q 'irc join.*#timeline-scroll' "$test_root/backend.log" 2>/dev/null; then
    ready=true
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.2
done
if [ "$ready" != true ]; then
  cat "$test_root/backend.log"
  cat "$test_root/fakeircd.log"
  echo "local Lurker + IRC test stack did not become ready" >&2
  exit 1
fi

line=0
while [ "$line" -lt 50 ]; do
  printf '#timeline-scroll :incoming IRC line #%s, written by bob over IRC\n' "$line" \
    | nc 127.0.0.1 16668
  line=$((line + 1))
done

ready=false
attempt=0
while [ "$attempt" -lt 100 ]; do
  if curl -fsS http://127.0.0.1:18081/api/state 2>/dev/null \
    | rg -q 'incoming IRC line #49'; then
    ready=true
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.2
done
if [ "$ready" != true ]; then
  cat "$test_root/backend.log"
  echo "IRC backlog did not reach the Lurker history store" >&2
  exit 1
fi

if [ "$#" -gt 0 ] && [ -n "$1" ]; then
  task test-apple-ui TEST_FILTER="LurkerUITests/LurkerUITests/$1"
else
  task test-apple-ui
fi
