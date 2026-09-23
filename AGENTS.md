# AGENTS.md

Guidance for AI coding agents working in the Esplanade repository.

## Project Overview

Esplanade is a cross-platform device continuity layer spanning Windows, macOS, Linux,
Android, and iOS. It provides file and photo transfer, clipboard sync, and device status
monitoring, integrating with each platform's native workflows rather than locking users
into a single ecosystem.

The project has two main components:

- **Go 1.23 REST API backend** — `server/`
- **Swift multi-platform client** — `apple/`, built around a shared `EsplanadeCore`
  package consumed by `EsplanadeiOS` and `EsplanadeMacOS`

## Agent Skills

Before starting a task, check `.skills/` for a relevant skill. Each skill lives in its own
subdirectory with a `SKILL.md` describing when and how to use it (e.g. adding a server
endpoint, MongoDB data access patterns). Skills encode the project's established
conventions — treat them as the first source of truth for how to do something, ahead of
inferring a pattern from surrounding code. When a skill's conventions and the current
codebase disagree, verify against the codebase and update the skill rather than
propagating stale guidance.

## Security Outlook

Esplanade treats security as a foundational constraint, not an add-on:

- **Local-first networking** — device-to-device communication is the default; nothing
  should route through infrastructure the user doesn't control unless there's no
  alternative.
- **End-to-end encryption** — data in transit between devices is encrypted by design,
  not opt-in.
- **No ecosystem lock-in** — this is a security and trust property as much as a product
  one; Esplanade should never require a proprietary cloud account or gate core
  functionality behind a vendor's ecosystem.

Agents should default to the more conservative security posture when a design choice
isn't fully specified, and flag (rather than silently resolve) any tradeoff that weakens
local-first or E2E guarantees.

## Go Backend Conventions

- Match existing patterns before writing new code: yaml tags on config structs,
  `ESPLANADE_`-prefixed env overrides, the middleware chain ordering (RequestID →
  Logging → Recovery → CORS), and the graceful shutdown sequence in `main.go`.
- Use `respond.JSON()` / `respond.Error()` for all HTTP responses — never write raw
  JSON.
- Repository layer: the generic `Repository[T, PT]` in `server/repository/repository.go`
  is the pattern for new resources — don't hand-roll Mongo calls.
- Route Mongo errors through `db/mongo/errors.go`'s `TranslateError()` rather than
  checking `mongo.Is...` directly.
- Known build gap: `go.sum` needs a local `go mod tidy` before the server compiles
  cleanly; no test scaffolding exists yet.

## Swift/iOS Conventions

- BLE code uses `@MainActor` isolation throughout — preserve that when extending
  `PeripheralServiceManager`, `PeripheralService`, or `PeripheralCharacteristic`.
- iOS design philosophy: deliberate minimalism. No filler UI (quick actions, stat cards,
  tips banners); favor native iOS interaction patterns; embrace empty space rather than
  filling it.
- Carry the "What → Where" interaction model (select content, then select a destination
  device) into other screens where it fits, rather than reinventing the flow
  per-feature.

## Coding Style: Comments

Comments are for information the code cannot express on its own — not a running
narration of what the code does.

- **Do not** add comments that restate what the next line obviously does (`// increment
  counter`, `// loop over items`).
- **Do** comment non-obvious constraints: why a magic number is what it is, why an edge
  case is handled a particular way, why a seemingly-simpler approach was rejected,
  concurrency/ordering requirements that aren't visible from the source code itself.
- If a comment would only explain *what* is happening rather than *why*, it belongs in
  the chat response to the user, a docs markdown file, or a relevant skill — not in the
  source file.
- When editing existing code, remove drive-by explanatory comments that no longer earn
  their place; don't leave stale commentary behind.
- Prefer making code self-explanatory (clear naming, small functions) over compensating
  with a comment.

## Branching & PR Policy for Agent Output

All code produced by an AI agent must land on a branch prefixed `vibes/...` (e.g.
`vibes/add-widgets-endpoint`) — this marks the branch as agent-generated and not yet
human-reviewed.

- Break work into small, focused branches and pull requests. Each pull request should
  represent a discrete, reviewable change; do not bundle unrelated concerns into one
  branch merely because they are part of the same larger effort.
- For dependent changes, agents may use GitHub stacked pull requests. The bottom branch
  should target the trunk (usually `main`), and each subsequent branch should target the
  branch immediately below it. Keep each layer independently reviewable, with lower
  branches containing foundational changes and higher branches containing changes that
  depend on them. Stacked pull requests must be merged from the bottom up.
- Any pull request opened from a `vibes/...` branch must be tagged with the `vibes`
  label on GitHub.
- Agents should never push agent-generated commits directly to `main` or to a
  human-owned feature branch.
- Once a human has reviewed and accepted the changes, it's on the human reviewer to
  decide whether to rename/merge out of the `vibes/` namespace — agents should not do
  this themselves.
