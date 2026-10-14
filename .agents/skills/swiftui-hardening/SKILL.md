---
name: swiftui-hardening
description: Reproduce and fix runtime, layout, hosting, scrolling, stale-state, focus, and hover failures in Lurker's macOS and iOS client. Use for live defects that unit tests miss.
license: MIT
metadata:
  version: "1.0"
  provenance: Independent project-specific synthesis informed by failure categories observed in tqbf/swiftui-app; no unlicensed source text/code copied; see ai-docs/apple-skills-notices.md.
---

# SwiftUI hardening

## Lurker context

Repository paths in this skill are relative to the Lurker root; `references/` and sibling skill links are relative to this skill. Read `AGENTS.md` and `ai-docs/apple.md`. The shared Xcode application targets macOS 26 and iOS 26. Use the existing `task` workflow. This is a private single-user client; preserve its endpoint policy and server-owned history.

Read `ai-docs/apple-pitfalls.md` before diagnosing a framework failure and record new reproduced workarounds there. For timeline changes, read `ai-docs/behaviors/timeline-scrolling.md`.

The macOS timeline uses TextKit 2 and hosted SwiftUI attachments. Never read `NSTextView.layoutManager`, which switches it to TextKit 1. Preserve native link hit-testing, cross-row selection, and scroll-follow behavior. The macOS composer also uses AppKit intentionally. iOS keeps its SwiftUI timeline and composer. A Preview-only sidebar failure is not evidence of a running-app failure.

Use `.claude/skills/verifier-apple/SKILL.md` for fixture and seeded-backend verification. Test rapid buffer switches, history prepends, delayed previews, resize, focus return, and app reactivation when relevant. Use an iPhone or iPad simulator/device for affected iOS behavior; macOS UI tests do not cover it.

For code changes, use the relevant `task format-apple`, `task lint-apple`, `task test-apple`, and `task test-apple-ui` targets and finish with `task build` as required by `AGENTS.md`. Read `.claude/skills/verifier-apple/SKILL.md` before live-app checks. Report unrun checks explicitly. Review-only and instruction-only work does not require an application build.

Read `references/runtime-hardening.md`. Use for crashes, layout breakage, frozen/stale UI, hover/list defects, restoration surprises, hosting issues, or any "works in tests but fails live" report.

## Workflow

1. Reproduce on the exact macOS or iOS minor version, hardware class, window size/state, input method, and hosting context.
2. Reduce to the smallest view/state transition while preserving the failure.
3. Classify it: structural identity/transition, layout proposal/priority, observation/cache, list/table/scroll container, storage/restoration, focus/hover, or AppKit/SwiftUI hosting mismatch.
4. Replace brittle composition with the simplest system-native invariant.
5. Add a deterministic logic regression test where possible and a documented live-run scenario where not.
6. Run repeated state changes, window resize/full-screen/tile changes, navigation and modal cycles, appearance, accessibility, focus, and affected-platform checks at the minimum supported OS version.
7. Record non-obvious failure modes and fixes in `ai-docs/apple-pitfalls.md` or the relevant behavior document.

## Guardrails

- A passing unit suite is not a passing app; live-run non-trivial view changes.
- Do not turn one observed framework bug into a universal rule without macOS-version and hosting evidence.
- Prefer stable view identity and dimensional/modifier changes when structural insertion/removal destabilizes hosted layouts.
- Rebuild snapshot-derived caches from the canonical source after mutations.
- Treat `@SceneStorage`/restoration defaults as persisted user state requiring migration.
- Hover-only actions must not reflow rows and need keyboard/menu alternatives.

## Output

Report the OS/hardware/window/input matrix, minimal trigger, classification, evidence, fix, regression protection, live-run results, and uncertainty about OS-specific behaviour.
