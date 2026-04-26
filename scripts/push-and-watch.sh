#!/usr/bin/env bash
set -euo pipefail

branch=$(git branch --show-current)
sha=$(git rev-parse HEAD)

wait_for_run_id() {
  local workflow=$1
  local branch_filter=$2
  local sha_filter=$3
  local attempts=${4:-60}
  local sleep_secs=${5:-5}

  local run_id
  for _ in $(seq 1 "$attempts"); do
    run_id=$(gh run list \
      --workflow "$workflow" \
      --branch "$branch_filter" \
      --commit "$sha_filter" \
      --limit 1 \
      --json databaseId \
      --jq '.[0].databaseId // ""')
    if [ -n "$run_id" ]; then
      printf '%s\n' "$run_id"
      return 0
    fi
    sleep "$sleep_secs"
  done

  echo "Timed out waiting for $workflow run for $sha_filter" >&2
  return 1
}

confirm_run_success() {
  local run_id=$1
  local expected_sha=$2
  local label=$3

  local result_sha result_conclusion
  result_sha=$(gh run view "$run_id" --json headSha --jq '.headSha // ""')
  result_conclusion=$(gh run view "$run_id" --json conclusion --jq '.conclusion // ""')

  if [ "$result_sha" != "$expected_sha" ]; then
    echo "$label run was for $result_sha, expected $expected_sha" >&2
    return 1
  fi
  if [ "$result_conclusion" != "success" ]; then
    echo "$label concluded with: $result_conclusion" >&2
    return 1
  fi
}

echo "Pushing $branch @ $sha"
git push

wait_for_completion() {
  local run_id=$1
  local label=$2
  local interval=${3:-5}

  while true; do
    local status conclusion url
    status=$(gh run view "$run_id" --json status --jq '.status // ""')
    conclusion=$(gh run view "$run_id" --json conclusion --jq '.conclusion // ""')
    url=$(gh run view "$run_id" --json url --jq '.url // ""')

    if [ "$status" = "completed" ]; then
      echo "$label completed with: ${conclusion:-unknown}"
      if [ -n "$url" ]; then
        echo "$label URL: $url"
      fi
      if [ "$conclusion" != "success" ]; then
        return 1
      fi
      return 0
    fi

    echo "$label status: ${status:-unknown}"
    sleep "$interval"
  done
}

echo "Waiting for Go CI on $branch @ $sha"
ci_run_id=$(wait_for_run_id "Go CI" "$branch" "$sha")
wait_for_completion "$ci_run_id" "Go CI"
confirm_run_success "$ci_run_id" "$sha" "Go CI"

if [ "$branch" = "main" ]; then
  echo "Waiting for Release on main @ $sha"
  release_run_id=$(wait_for_run_id "Release" "main" "$sha" 120 10)
  wait_for_completion "$release_run_id" "Release" 10
  confirm_run_success "$release_run_id" "$sha" "Release"
else
  echo "Skipping Release watch: release workflow only runs from main"
fi

echo "Push verified for $sha"
