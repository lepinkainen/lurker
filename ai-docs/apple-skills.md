# Apple development skills

The repository includes six adapted skills under `.agents/skills/`. Load the
skill that matches the work; ordinary Apple changes do not require every review.

[Issue #168](https://github.com/lepinkainen/lurker/issues/168) tracks the nine
remaining upstream skills, including why each is deferred or unsuitable.

| Skill | Use |
| --- | --- |
| [swiftui-hardening](../.agents/skills/swiftui-hardening/SKILL.md) | Reproduce runtime, hosting, layout, scrolling, focus, and stale-state defects. |
| [swift-concurrency](../.agents/skills/swift-concurrency/SKILL.md) | Review actor isolation, task ownership, cancellation, hydration, and stale responses. |
| [swiftui-performance](../.agents/skills/swiftui-performance/SKILL.md) | Measure rendering, scrolling, images, memory, and energy before changing implementation. |
| [apple-accessibility](../.agents/skills/apple-accessibility/SKILL.md) | Check custom timeline rows, VoiceOver, keyboard focus, text scaling, and touch interaction. |
| [swift-testing](../.agents/skills/swift-testing/SKILL.md) | Add deterministic model and transport tests, plus UI tests where behavior requires them. |
| [apple-design-review](../.agents/skills/apple-design-review/SKILL.md) | Review native layout and interaction across macOS, iPhone, and iPad. |

## Local adaptations

These skills derive from `rcarmo/swift-app-template` at revision
`b860498d1455979a61fb50800e2d56221d6d175f`. See
[licenses and provenance](apple-skills-notices.md) for the source license and
upstream notices. Keep the reference files and `agents/openai.yaml` metadata
with each skill when updating it.

The upstream template uses SwiftPM, Make, and macOS-only application
assumptions. Lurker uses its existing Xcode project and `task` commands, with
shared macOS 26 and iOS 26 sources. The adaptations preserve explicit
`@MainActor` annotations, the `LurkerAPI` actor, and the intentional AppKit
timeline and composer. They do not enable default MainActor isolation or add
compiler feature flags.

The skills point to [Apple architecture](apple.md),
[reproduced pitfalls](apple-pitfalls.md), and the existing
[Apple verifier](../.claude/skills/verifier-apple/SKILL.md).
The verifier supplies runtime procedures; the testing skill adds deterministic
coverage guidance. macOS UI test results do not establish iOS correctness.

Source references to uninstalled navigation and typography skills were
replaced with Lurker's existing navigation and theme guidance. Future updates
must preserve these local choices and keep all reference paths valid.
