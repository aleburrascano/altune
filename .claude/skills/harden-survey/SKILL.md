---
name: harden-survey
description: Survey ONE module for robustness/correctness/security defects and emit curated bug and type:security tickets. Read-only — tickets are the output, never code. Fans out a focused pass per robustness domain-group so each goes deep. Use per module to harden before expanding. Trigger words: harden, harden-survey, robustness pass, find defects in <module>, security pass.
---

# Harden survey

**Robustness — these fixes CHANGE behaviour, to make it correct or safe.** Runs the shared survey engine with the robustness lens below. Output: `bug` and `type:security` tickets.

**Follow [`.claude/survey-engine.md`](../../survey-engine.md)** with everything here.

## Ticket type & Done-when
- Labels: **`bug`** (the default label — NOT `type:bug`, which does not exist) for correctness/robustness; **`type:security`** for a security hole. Title prefix `[Bug]:` or `[Security]:`.
- **Done-when** (the body template): a test **reproduces** the defect (red) → the fix makes it green → a regression test stays · gate green. A high-severity security defect gets `ready`.

## Lens — fan out ONE focused pass per domain group
Doctrine: `~/.claude/workflow/robustness-domains.md` is the authoritative domain list — each pass walks its group's *Applies-when* / *Done-when* columns and reports live-but-unhandled domains as defects (with severity).

1. **Input & trust** — Parsing, Validation, Normalization, Injection.
2. **Authority** — Authentication, Authorization, Tenancy, Confused deputy.
3. **Correctness** — Invariants, Totality, Arithmetic, Time.
4. **Failure & state** — Classification, Fallback, Retry, Containment, Partial failure; Atomicity, Idempotency, Concurrency, Ordering, Consistency, Migration.
5. **Resources & load** — Bounds, Timeouts, Backpressure, Lifecycle; Capacity, Degradation.
6. **Privacy, operability & interface** — Secrets, PII lifecycle, Log hygiene; Diagnosability, Metrics, Correlation, Kill switch; Compatibility, Versioning, Error contract, Configuration.

Each defect reports **severity** (high/medium/low). A pure structure fix (no behaviour change) is a `refactor` ticket, not this.
