---
name: swift-concurrency
description: Implement or review Swift concurrency in Lurker's Apple client, including WebSocket tasks, hydration, cancellation, actor isolation, image loading, and stale responses.
license: MIT
metadata:
  version: "1.0"
  provenance: Adapted from twostraws Swift guidance and project-specific Swift 6 policy; see ai-docs/apple-skills-notices.md.
---

# Swift concurrency

## Lurker context

Repository paths in this skill are relative to the Lurker root; `references/` and sibling skill links are relative to this skill. Read `AGENTS.md` and `ai-docs/apple.md`. The shared Xcode application targets macOS 26 and iOS 26. Use the existing `task` workflow. This is a private single-user client; preserve its endpoint policy and server-owned history.

Read `apple/Lurker.xcodeproj/project.pbxproj` for current compiler settings. Lurker uses Swift 6 language mode and complete concurrency checking with explicit `@MainActor` annotations. Do not assume default MainActor isolation or template-specific upcoming feature flags. Preserve `AppModel` isolation and the `LurkerAPI` actor boundary.

Preserve stream-before-snapshot hydration and queued-event replay. Review cancellation and stale writes when replacing the server, switching buffers, searching, or reconnecting. Image downloads belong to the shared cache and intentionally outlive individual scrolling rows; canceling a row must not discard a download needed by other rows. Review cache lifetime separately from subscriber lifetime.

For code changes, use the relevant `task format-apple`, `task lint-apple`, `task test-apple`, and `task test-apple-ui` targets and finish with `task build` as required by `AGENTS.md`. Read `.claude/skills/verifier-apple/SKILL.md` before live-app checks. Report unrun checks explicitly. Review-only and instruction-only work does not require an application build.

Read `references/concurrency-checklist.md` when changing asynchronous services, observable models, streams, actors, or task lifetimes.

## Workflow

1. Identify mutable state and assign one isolation domain.
2. Keep UI-facing state on `@MainActor`.
3. Make dependency protocols and cross-task values `Sendable`.
4. Prefer structured `async` functions and task groups to callbacks or detached work.
5. Define which scope owns each `Task` and when cancellation occurs.
6. Propagate cancellation; do not convert it into a user-facing failure accidentally.
7. Prevent stale responses from overwriting newer state.
8. Inject clocks or continuations for deterministic tests instead of sleeping.
9. Run `task test-apple` and `task build`; fix diagnostics at the isolation boundary rather than weakening Xcode settings or using broad suppressions.

## Reject

- `DispatchQueue.main.async` and global queues in new Swift concurrency code.
- `Task.sleep(nanoseconds:)`; use `Task.sleep(for:)` or an injected clock.
- `Task.detached` without a documented isolation/priority reason.
- mutable shared state outside an actor/global actor/explicit synchronisation mechanism.
- broad `@unchecked Sendable`; a narrow suppression requires a written thread-safety proof.
- retaining unstructured tasks without cancellation or lifecycle ownership.
- swallowing errors or cancellation.

## Review output

State the isolation of each mutable object, the task owner, cancellation path, and any value crossing an actor boundary. Prioritise data races and stale writes over stylistic concerns.
