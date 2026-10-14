---
name: swiftui-performance
description: Measure and diagnose scrolling, rendering, image, memory, and energy problems in Lurker's macOS TextKit timeline and iOS SwiftUI client.
license: MIT
metadata:
  version: "1.0"
  provenance: Adapted from twostraws performance guidance and project-specific SwiftUI profiling practice; see ai-docs/apple-skills-notices.md.
---

# SwiftUI performance

## Lurker context

Repository paths in this skill are relative to the Lurker root; `references/` and sibling skill links are relative to this skill. Read `AGENTS.md` and `ai-docs/apple.md`. The shared Xcode application targets macOS 26 and iOS 26. Use the existing `task` workflow. This is a private single-user client; preserve its endpoint policy and server-owned history.

Read `ai-docs/behaviors/timeline-scrolling.md` before changing scrolling. Profile macOS TextKit 2 layout, block diffs, attributed text, hosted previews, and avatar updates separately from the iOS SwiftUI lazy timeline. Preserve the AppKit implementation and its link/copy/accessibility behavior.

Measure realistic backlog sizes, history prepends, burst arrivals, delayed images, and buffer switches. Shared image downloads intentionally survive row disappearance. In performance guidance, SwiftUI's time-based `TimelineView` API is distinct from Lurker's message-timeline view with the same name.

For code changes, use the relevant `task format-apple`, `task lint-apple`, `task test-apple`, and `task test-apple-ui` targets and finish with `task build` as required by `AGENTS.md`. Read `.claude/skills/verifier-apple/SKILL.md` before live-app checks. Report unrun checks explicitly. Review-only and instruction-only work does not require an application build.

Read `references/performance-playbook.md`. Use for slow rendering, scrolling hitches, repeated work, memory growth, startup delay, excessive body updates, battery/thermal issues, or performance-focused review.

## Workflow

1. Reproduce with representative Mac, iPhone, or iPad hardware and OS version, window configuration, data set, and release/debug build as appropriate.
2. Record a baseline metric or trace; do not optimise from code appearance alone.
3. Use Instruments (SwiftUI, Time Profiler, Allocations/Leaks, Core Animation, Network, Energy) and signposts where appropriate.
4. Identify the invalidation source, expensive transform, I/O boundary, allocation, layout cycle, image cost, or task storm.
5. Apply the smallest correction while preserving behaviour and accessibility.
6. Re-measure under the same conditions and add a regression benchmark/test where stable.
7. Build and run on affected macOS 26 or iOS 26 targets with representative hardware; record any unavailable platform measurements.

## Guardrails

- Do not add `EquatableView`, caches, manual memoization, or flattened view trees without evidence.
- Do not move work to a background task unless ownership, cancellation, actor isolation, and UI commit are correct.
- Avoid `AnyView`, unstable IDs, eager stacks for large data, expensive transforms in `body`, and formatter creation when format styles work.
- Scope timers/`TimelineView` and observation to the smallest subtree.
- Cache only with measured benefit and explicit invalidation/memory bounds.
- Performance changes must not remove labels, text scaling, reduced-motion behaviour, keyboard/focus support, or native macOS semantics.

## Output

Report reproduction, baseline, trace/tool, root cause, change, after measurement, variance, tested hardware, OS, and window or orientation state, and remaining risks. Separate measured facts from hypotheses.
