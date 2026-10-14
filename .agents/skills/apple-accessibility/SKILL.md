---
name: apple-accessibility
description: Implement or review accessibility in Lurker's macOS and iOS client, including custom timeline rows, VoiceOver, keyboard focus, text scaling, and contrast.
license: MIT
metadata:
  version: "2.0"
  provenance: Adapted from twostraws accessibility guidance and macOS review practice; see ai-docs/apple-skills-notices.md.
---

# Apple accessibility

## Lurker context

Repository paths in this skill are relative to the Lurker root; `references/` and sibling skill links are relative to this skill. Read `AGENTS.md` and `ai-docs/apple.md`. The shared Xcode application targets macOS 26 and iOS 26. Use the existing `task` workflow. This is a private single-user client; preserve its endpoint policy and server-owned history.

The macOS timeline exposes message rows through `TimelineAXHostView`. Preserve its retained accessibility elements and sender/time/content labels. A loaded AX row is not necessarily visible; check frames for viewport assertions. iOS combines message-row content into one accessibility element.

Check composer focus, autocomplete, Command-K, search, unread-bar activation, member actions, and attachment alternatives when affected. Test iOS Dynamic Type, touch targets, VoiceOver, compact width, and the onscreen keyboard as well as macOS keyboard and pointer input. Keep hover-only controls available without hover on iOS.

For code changes, use the relevant `task format-apple`, `task lint-apple`, `task test-apple`, and `task test-apple-ui` targets and finish with `task build` as required by `AGENTS.md`. Read `.claude/skills/verifier-apple/SKILL.md` before live-app checks. Report unrun checks explicitly. Review-only and instruction-only work does not require an application build.

Read the applicable sections of `references/accessibility-checklist.md` when implementing or reviewing accessible controls, message content, focus, or input workflows.

## Workflow

1. Define the label, value, hint, traits, actions, grouping, and heading level before visual styling.
2. Prefer native controls, which provide system behaviour and accessibility semantics.
3. Test flexible layout with the largest supported macOS text and accessibility settings.
4. Verify VoiceOver order, focus, actions, and announcements.
5. Verify that Voice Control uses visible, stable names.
6. Check light and dark appearance, Increase Contrast, Reduce Transparency, and Differentiate Without Color.
7. Check Reduce Motion for spatial or repeated animation.
8. Test keyboard navigation, Full Keyboard Access, pointer and hover, menu and toolbar access, drag-and-drop alternatives, and focus after state changes.
9. Record manual checks that cannot be automated.

## Guardrails

- An icon-only button still needs a text label. Use `Button("Label", systemImage: ...)` and apply `.labelStyle(.iconOnly)` only to the visual presentation.
- Hide decorative images. Give meaningful images concise labels or descriptions.
- Use `Button` for ordinary actions. Use gestures only when tap count, position, or movement is part of the action, and add matching accessibility actions.
- Never communicate state through colour alone.
- Pointer targets must be easy to acquire, and every action needs a keyboard and accessibility-focus path.
- Use semantic text styles. Scale any necessary custom dimensions with the relevant text style.
- Provide a reduced-motion alternative for large spatial movement.

## Output

Report the affected assistive technology or setting, OS version, window or orientation state, consequence, correction, and exact manual verification steps. Classify unreachable or unnamed actions as blockers.
