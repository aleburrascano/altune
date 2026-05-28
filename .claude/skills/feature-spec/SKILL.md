---
name: feature-spec
description: |
  ALWAYS fires when the user describes a new feature, says "spec out", "let's spec", "write a spec for",
  "design a feature", "requirements for", or describes user-facing functionality that doesn't exist yet.
  Walks the user through framing a feature as a testable spec, writes docs/specs/<feat>/spec.md from the
  template, and dispatches the spec-reviewer subagent for an explicit clarify-gate before moving to plan.
  This is step 1 of the feature loop.
when_to_use: |
  Use BEFORE any /feature-plan or implementation. If the user starts implementing without a spec, stop
  them and run this first. If a spec exists but is unclear, use it to revise.
---

# Feature spec

## Mandatory first step

Query the software-architecture-design vault MCP:
1. `mcp__software-architecture-design__vk_search` for the **kind of feature** being specced (e.g., "saga" for async multi-step, "event sourcing" for audit-heavy, "CQRS" for read/write asymmetry, "REST" / "GraphQL" if API-shape question).
2. `mcp__software-architecture-design__vk_get_note` on top 2–3 hits.
3. Surface relevant patterns + trade-offs in the spec's "Design considerations" section.

If no relevant vault note: `"vault returned no matches for <topic>"` — proceed but flag in the spec.

## What this skill does

1. **Confirm scope**. Ask 3 questions max:
   - What user problem does this solve? (1 sentence)
   - What's explicitly *out* of scope?
   - What existing feature/bounded context does this touch or sit next to?
   Then state the **minimal tier**: the smallest end-to-end version that delivers the value, and list what infra/resilience/scale is deferred to post-launch. Default solo/pre-launch features to minimal — heavy infra needs a one-line "needed now because …" or it's deferred.
2. **Decide folder name**. Kebab-case, ≤25 chars, matches a bounded context or sits cleanly under one. Confirm with user.
3. **Create directory** `docs/specs/<feat>/` and copy `docs/specs/_template/spec.md` → `docs/specs/<feat>/spec.md`.
4. **Fill the spec** from the template — see `resources/template-fields.md` for guidance on each section.
5. **Append the new scope** to `commitlint.config.js` `scope-enum` (so commits like `feat(<feat>): …` validate).
6. **Dispatch spec-reviewer subagent** with `docs/specs/<feat>/spec.md` for clarify-gate review. Block on its output — present findings to user, revise.
7. **Hand off** to `/feature-plan <feat>` only after the spec passes review.

## Sections every spec must have

- **Problem** — the user pain in 2–4 sentences. No solutions yet.
- **User value** — what changes for the user when this ships.
- **Scope tier / MVP cut** — the minimal shippable version; what infra is deferred to post-launch. Solo/pre-launch defaults to minimal. ACs cover the minimal tier only.
- **Acceptance criteria** — testable, numbered. Each one is a future test name.
- **Out of scope** — explicit non-goals.
- **Design considerations** — vault references + relevant patterns; high-level approach (e.g., "this is a CQRS read path; writes already exist in catalog context").
- **Dependencies** — features, bounded contexts, third-party services this needs.
- **Risks / open questions** — what could go wrong; what's still unknown.
- **Telemetry** — what we'd log/measure to know this works in production.

## Anti-patterns

- Specs that describe implementation ("we'll use SQLAlchemy with a join…"). The spec is *what + why*, not *how*. How lives in the plan.
- Specs without testable acceptance criteria ("works well", "is fast"). Quantify or omit.
- Multi-thousand-word specs. Decompose into multiple features.
- **Enterprise infra before launch.** Caching, multi-provider scatter-gather, dedup engines, circuit breakers, rate limiting, telemetry alerts in a pre-launch solo app. Default these to the "Deferred to post-launch" tier unless there's a concrete "needed now because …". Shipping user value to measure beats de-risking scale that may never arrive.
- Speccing during a session also dedicated to implementation. Specs are stage 1; if you're spec'ing, you're spec'ing — implementation is a separate session.

## Resources

- `resources/template-fields.md` — guidance per section
- `docs/specs/_template/spec.md` — the source template
