---
name: swift-testing
description: Write or review deterministic Swift Testing coverage for Lurker's Apple models, transports, parsing, and regressions. Use XCUITest for interactions that need the running app.
license: MIT
metadata:
  version: "2.0"
  provenance: Adapted from twostraws hygiene/view guidance, Airbnb testing guidance, and this template's test strategy; see ai-docs/apple-skills-notices.md.
---

# Swift Testing

## Lurker context

Repository paths in this skill are relative to the Lurker root; `references/` and sibling skill links are relative to this skill. Read `AGENTS.md` and `ai-docs/apple.md`. The shared Xcode application targets macOS 26 and iOS 26. Use the existing `task` workflow. This is a private single-user client; preserve its endpoint policy and server-owned history.

Use `apple/LurkerTests/` for Swift Testing and `apple/LurkerUITests/` for XCTest UI automation. Extend `FixtureTransport` or a small test-specific transport for deterministic responses. Isolate `UserDefaults` per test. The `-ui-testing` launch argument selects fixture data and suppresses notification permission prompts.

Read `.claude/skills/verifier-apple/SKILL.md` for running-app evidence. Test target hosts are macOS-only. An iOS build or simulator/device check must be reported separately. Preserve the user's running client and real preferences; use launch-argument overrides for a test endpoint.

For code changes, use the relevant `task format-apple`, `task lint-apple`, `task test-apple`, and `task test-apple-ui` targets and finish with `task build` as required by `AGENTS.md`. Read `.claude/skills/verifier-apple/SKILL.md` before live-app checks. Report unrun checks explicitly. Review-only and instruction-only work does not require an application build.

Read `ai-docs/testing-and-build.md` and `references/testing-patterns.md`. Identify test seams before implementation and use tests afterward as one part of the evidence.

## Workflow

1. Translate requirements into observable behaviour and invariants.
2. Test pure domain logic first.
3. Inject deterministic service fakes into observable models.
4. Cover only applicable transitions: initial, loading, success, empty, failure, retry, cancellation, and stale response.
5. Fix IDs, dates, locales, calendars, time zones, and clocks.
6. Add isolated integration tests when persistence, network, filesystem, or Keychain adapters are introduced.
7. Add UI automation only for critical journeys or behaviour that cannot be verified below the UI.
8. Run `task test-apple` and `task build`. Use the Apple verifier for affected UI, focus, window, accessibility, entitlement, or bundle behavior; verify iOS changes on that platform too.

## Guardrails

- Use Swift Testing for new tests unless a required framework API needs XCTest.
- Avoid force unwraps and force tries in tests; use `#require` or throwing helpers with useful context.
- Do not sleep or depend on wall-clock timing.
- Do not share mutable fixture state between tests.
- Test behaviour rather than private implementation, SwiftUI body shape, or incidental call counts.
- A passing suite does not prove navigation, layout, focus, animation, signing, permissions, or bundle metadata.
- Do not claim generic checklist states as covered unless a test or recorded manual run verifies them.

## Output

List behaviour covered, fixtures and dependencies, deterministic controls, commands run, macOS assembly/manual checks, and remaining gaps. A regression fix should include a test that fails without the fix when practical.
