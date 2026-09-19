#!/bin/sh
set -eu

test_root=$(mktemp -d)
sender_pid=
backend_pid=

cleanup() {
  if [ -n "$backend_pid" ]; then
    kill "$backend_pid" 2>/dev/null || true
    wait "$backend_pid" 2>/dev/null || true
  fi
  if [ -n "$sender_pid" ]; then
    kill "$sender_pid" 2>/dev/null || true
    wait "$sender_pid" 2>/dev/null || true
  fi
  rm -rf "$test_root"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

ergo_addr=${ERGO_ADDR:-127.0.0.1:16667}
ergo_host=${ergo_addr%:*}
ergo_port=${ergo_addr##*:}

cat >"$test_root/config.yaml" <<EOF
networks:
  - network: IRCUITest
    nick: lurker
    channels:
      - "#timeline-scroll"
    servers:
      - host: $ergo_host
        port: $ergo_port
        tls: false
EOF

go build -o "$test_root/irctestclient" ./cmd/irctestclient
go build -o "$test_root/lurker" .

"$test_root/irctestclient" -addr "$ergo_addr" -channel '#timeline-scroll' \
  >"$test_root/sender.log" 2>&1 &
sender_pid=$!

DATA_DIR="$test_root/data" CONFIG_PATH="$test_root/config.yaml" ADDR=127.0.0.1:18081 \
  "$test_root/lurker" >"$test_root/backend.log" 2>&1 &
backend_pid=$!

ready=false
attempt=0
while [ "$attempt" -lt 100 ]; do
  if curl -fsS http://127.0.0.1:18081/whoami >/dev/null 2>&1 \
    && curl -fsS http://127.0.0.1:16668/ready >/dev/null 2>&1 \
    && rg -q 'irc join.*#timeline-scroll' "$test_root/backend.log" 2>/dev/null; then
    ready=true
    break
  fi
  attempt=$((attempt + 1))
  sleep 0.2
done
if [ "$ready" != true ]; then
  cat "$test_root/backend.log"
  cat "$test_root/sender.log"
  echo "local Lurker + IRC test stack did not become ready" >&2
  exit 1
fi

line=0
while [ "$line" -lt 50 ]; do
  curl -fsS --max-time 5 http://127.0.0.1:16668/message \
    --data-binary "incoming IRC line #$line, written by bob over IRC"
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
  tests=$1
else
  # Live tests opt in by name; setUp() keys off the same "LiveIRC" marker to
  # add the live launch arguments, so new ones are picked up without editing
  # this script.
  tests=$(rg -o 'func (test[A-Za-z0-9_]*LiveIRC[A-Za-z0-9_]*)' -r '$1' \
    apple/LurkerUITests/LurkerUITests.swift)
fi
if [ -z "$tests" ]; then
  echo "no live UI tests matched" >&2
  exit 1
fi
for test_name in $tests; do
  task test-apple-ui TEST_FILTER="LurkerUITests/LurkerUITests/$test_name"
done
