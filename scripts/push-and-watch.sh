#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# ///
"""Push the current branch and wait for matching GitHub Actions workflows."""

from __future__ import annotations

import json
import subprocess
import sys
import time
from dataclasses import dataclass


CI_WORKFLOW_NAME = "Go CI"
RELEASE_WORKFLOW_NAME = "Release"


@dataclass
class Run:
    database_id: int
    head_sha: str
    workflow_name: str
    created_at: str
    status: str = ""
    conclusion: str = ""
    url: str = ""


class CommandError(RuntimeError):
    pass


def run_command(*args: str, capture_output: bool = True) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(args, text=True, capture_output=capture_output)
    if result.returncode != 0:
        stderr = result.stderr.strip()
        stdout = result.stdout.strip()
        message = stderr or stdout or f"command failed: {' '.join(args)}"
        raise CommandError(message)
    return result


def command_output(*args: str) -> str:
    return run_command(*args).stdout.strip()


def current_branch() -> str:
    return command_output("git", "branch", "--show-current")


def current_sha() -> str:
    return command_output("git", "rev-parse", "HEAD")


def is_remote_at_head(remote_ref: str, sha: str) -> bool:
    verify = subprocess.run(
        ["git", "rev-parse", "--verify", remote_ref],
        text=True,
        capture_output=True,
    )
    if verify.returncode != 0:
        return False
    return verify.stdout.strip() == sha


def push_urls(remote: str) -> list[str]:
    result = subprocess.run(
        ["git", "config", "--get-all", f"remote.{remote}.pushurl"],
        text=True,
        capture_output=True,
    )
    urls = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    if urls:
        return urls
    fetch_url = subprocess.run(
        ["git", "config", "--get", f"remote.{remote}.url"],
        text=True,
        capture_output=True,
    )
    url = fetch_url.stdout.strip()
    return [url] if url else []


def push_current_branch(branch: str, sha: str) -> None:
    print(f"Pushing {branch} @ {sha}")
    urls = push_urls("origin")
    if not urls:
        result = subprocess.run(["git", "push", "origin", branch], text=True)
        if result.returncode != 0:
            raise SystemExit(result.returncode)
        return

    primary, mirrors = urls[0], urls[1:]
    primary_result = subprocess.run(
        ["git", "push", primary, branch],
        text=True,
    )
    if primary_result.returncode != 0:
        raise SystemExit(primary_result.returncode)

    for url in mirrors:
        mirror_result = subprocess.run(
            ["git", "push", url, branch],
            text=True,
        )
        if mirror_result.returncode != 0:
            print(f"warning: push to mirror {url} failed (exit {mirror_result.returncode})", file=sys.stderr)


def list_runs(limit: int = 100) -> list[Run]:
    output = command_output(
        "gh",
        "run",
        "list",
        "--limit",
        str(limit),
        "--json",
        "databaseId,headSha,workflowName,createdAt,status,conclusion,url",
    )
    items = json.loads(output)
    return [
        Run(
            database_id=item["databaseId"],
            head_sha=item.get("headSha", ""),
            workflow_name=item.get("workflowName", ""),
            created_at=item.get("createdAt", ""),
            status=item.get("status", ""),
            conclusion=item.get("conclusion", ""),
            url=item.get("url", ""),
        )
        for item in items
    ]


def find_run(workflow_name: str, sha: str) -> Run | None:
    matches = [run for run in list_runs() if run.workflow_name == workflow_name and run.head_sha == sha]
    if not matches:
        return None
    matches.sort(key=lambda run: run.created_at)
    return matches[-1]


def wait_for_run(workflow_name: str, sha: str, attempts: int = 60, sleep_secs: int = 5) -> Run:
    for attempt in range(1, attempts + 1):
        print(
            f"Looking for {workflow_name} run for {sha} (attempt {attempt}/{attempts})",
            file=sys.stderr,
        )
        run = find_run(workflow_name, sha)
        if run is not None:
            print(f"Found {workflow_name} run: {run.database_id}", file=sys.stderr)
            return run
        time.sleep(sleep_secs)
    raise SystemExit(f"Timed out waiting for {workflow_name} run for {sha}")


def view_run(run_id: int) -> Run:
    output = command_output(
        "gh",
        "run",
        "view",
        str(run_id),
        "--json",
        "databaseId,headSha,workflowName,createdAt,status,conclusion,url",
    )
    item = json.loads(output)
    return Run(
        database_id=item["databaseId"],
        head_sha=item.get("headSha", ""),
        workflow_name=item.get("workflowName", ""),
        created_at=item.get("createdAt", ""),
        status=item.get("status", ""),
        conclusion=item.get("conclusion", ""),
        url=item.get("url", ""),
    )


def wait_for_completion(run_id: int, label: str, interval: int = 5) -> Run:
    elapsed = 0
    while True:
        run = view_run(run_id)
        if run.status == "completed":
            conclusion = run.conclusion or "unknown"
            print(f"{label} completed with: {conclusion} after {elapsed}s")
            if run.url:
                print(f"{label} URL: {run.url}")
            if run.conclusion != "success":
                raise SystemExit(1)
            return run

        status = run.status or "unknown"
        print(f"{label} status: {status} (elapsed: {elapsed}s, checking again in {interval}s)")
        time.sleep(interval)
        elapsed += interval


def confirm_run_success(run_id: int, expected_sha: str, label: str) -> None:
    run = view_run(run_id)
    if run.head_sha != expected_sha:
        raise SystemExit(f"{label} run was for {run.head_sha}, expected {expected_sha}")
    if run.conclusion != "success":
        raise SystemExit(f"{label} concluded with: {run.conclusion}")


def main() -> int:
    branch = current_branch()
    sha = current_sha()
    remote_ref = f"origin/{branch}"

    push_current_branch(branch, sha)

    if is_remote_at_head(remote_ref, sha):
        print(f"{remote_ref} already at {sha}")

    print(f"Waiting for Go CI on {branch} @ {sha}")
    ci_run = find_run(CI_WORKFLOW_NAME, sha)
    if ci_run is not None:
        print(f"Found existing Go CI run: {ci_run.database_id}")
    else:
        ci_run = wait_for_run(CI_WORKFLOW_NAME, sha)
    wait_for_completion(ci_run.database_id, "Go CI")
    confirm_run_success(ci_run.database_id, sha, "Go CI")

    if branch == "main":
        print(f"Waiting for Release on main @ {sha}")
        release_run = find_run(RELEASE_WORKFLOW_NAME, sha)
        if release_run is not None:
            print(f"Found existing Release run: {release_run.database_id}")
        else:
            release_run = wait_for_run(RELEASE_WORKFLOW_NAME, sha, attempts=120, sleep_secs=10)
        wait_for_completion(release_run.database_id, "Release", interval=10)
        confirm_run_success(release_run.database_id, sha, "Release")
    else:
        print("Skipping Release watch: release workflow only runs from main")

    print(f"Push verified for {sha}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except CommandError as exc:
        print(exc, file=sys.stderr)
        raise SystemExit(1)
