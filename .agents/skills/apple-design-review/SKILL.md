---
name: apple-design-review
description: Design or review Lurker's native macOS and iOS layouts and interactions, including the sidebar, composer, search, inspectors, menus, and keyboard commands.
license: MIT
metadata:
  version: "3.0"
  provenance: Adapted from ceorkm/macos-design-skill, twostraws design guidance, and project-specific macOS rules; see ai-docs/apple-skills-notices.md.
---

# Apple design review

## Lurker context

Repository paths in this skill are relative to the Lurker root; `references/` and sibling skill links are relative to this skill. Read `AGENTS.md` and `ai-docs/apple.md`. The shared Xcode application targets macOS 26 and iOS 26. Use the existing `task` workflow. This is a private single-user client; preserve its endpoint policy and server-owned history.

Read `ai-docs/keyboard-shortcuts.md` when commands or focus change, and `ai-docs/apple-pitfalls.md` for layout/hosting constraints. Reuse `Theme.swift` roles and `Platform.swift` helpers. Preserve intentional AppKit boundaries for the macOS timeline and composer.

macOS uses one main `Window`, a `Settings` scene, menus, and an inspector. iPad regular width uses split navigation; iPhone compact width uses a stack and sheets. iOS settings are an in-app sheet. Review selection and focus through collapse, channel switching, search, notification taps, and modal dismissal. Keep iOS touch and keyboard behavior explicit; do not add desktop window chrome to shared views.

For code changes, use the relevant `task format-apple`, `task lint-apple`, `task test-apple`, and `task test-apple-ui` targets and finish with `task build` as required by `AGENTS.md`. Read `.claude/skills/verifier-apple/SKILL.md` before live-app checks. Report unrun checks explicitly. Review-only and instruction-only work does not require an application build.

Read the references needed for the task:

- `references/layout-and-composition.md`
- `references/visual-system.md`
- `references/interaction-patterns.md`
- `references/platform-matrix.md`

Use `../apple-accessibility/SKILL.md` for accessibility work. For navigation changes, inspect `RootView.swift` and selection/presentation state in `AppModel` before proposing new routing abstractions.

## Workflow

1. Name the primary user task and content hierarchy.
2. Choose system windows, scenes, navigation, toolbars, lists, tables, grids, forms, and presentation containers.
3. Cover loading, populated, empty, search-empty, failure, permission-denied, offline, and absent-selection states.
4. Define pointer, keyboard, VoiceOver, Voice Control, menu/command, drag-and-drop, and multi-window behaviour where applicable.
5. Start with semantic system typography, colour, materials, symbols, spacing, and controls.
6. Review narrow, standard, wide, full-screen, active, and inactive window states.
7. Check light and dark appearance, localisation expansion, text scaling, contrast, Reduce Transparency, and Reduce Motion.
8. Use previews for representative states, then run the assembled app on the minimum supported OS versions on affected platforms with representative input devices.

## Native composition

Lurker uses shared SwiftUI with deliberate macOS AppKit components. Use system title-bar, toolbar, window, material, colour, and control APIs. Do not draw traffic lights, fake title bars, copy browser or Electron chrome, or convert CSS measurements into fixed SwiftUI metrics.

## Output

Group findings into blockers, important improvements, and polish. Name the OS version, window size or device orientation, input method, and affected application state. Give the consequence and smallest correction for each finding.
