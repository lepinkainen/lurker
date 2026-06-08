# Behaviors

Runtime behavior specs that are hard or impossible to cover with unit/Playwright tests. Each file describes one feature cluster.

Format per file:

- **Purpose** — why the behavior exists, user-facing intent.
- **Trigger** — what starts the behavior.
- **Expected** — what should happen (golden path).
- **Cases** — numbered scenarios (A, B, …) with step-by-step expected state.
- **Edge cases** — boundary conditions, race conditions, things easy to get wrong.
- **Non-goals** — what this behavior explicitly does not do.
- **Related** — links to code, other behaviors, architecture docs.

These docs are authoritative when implementation and tests disagree. Update the doc first when intent changes, then the code.
