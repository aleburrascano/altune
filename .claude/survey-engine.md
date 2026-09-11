# Survey engine

The shared procedure the per-module survey skills run (`refactor-survey`, `harden-survey`). The calling skill supplies three things: its **lens** (a list of focused category-passes), its **ticket type**, and its **Done-when**. This engine does the fan-out and emits the tickets. **Read-only — the tickets are the only output.**

## Input
One module path per run. If none is given, ask; never survey the whole repo at once.

## 1. Fan out — one focused pass per category (depth by focus)
Dispatch a **separate read-only sub-agent per category the skill lists**, concurrently, over the module. Give each agent **only its own category's lens** — a narrow pass goes deep where one broad pass skims. This is the whole point: focus buys depth.

Each sub-agent must:
- **Grep and count.** Every finding cites a count — "N copies at paths:lines", "0 non-test callers", "an X-line function". A count is the reproducible anchor; no count, no finding.
- **Verify against source** before reporting — no speculation.
- Apply the counter-lens: report only if it clears the category's threshold **and** names the do-nothing alternative and when do-nothing wins. If do-nothing wins → **SOUND**, not a finding.
- Return findings in a `PACKAGE:`/`DEFECT:` block with evidence + counts, plus a short SOUND list.

## 2. Aggregate + curate
- Collect every pass's findings.
- Drop `never` / low-value; bundle tightly-related micro-items into one ticket.
- **Dedupe** against open tickets of this type: `gh issue list --label <ticket-type> --state open --limit 100`. Never recreate one.
- Prefer earned tickets over makework — stop at earned findings, don't chase a module to zero.

## 3. Emit

First capture the survey commit and repo once: `sha=$(git rev-parse HEAD)`, `repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)`.

**Every evidence location is a SHA-pinned permalink**, so the ticket *shows* the code instead of pointing at a path to go hunt:
- Convert each `path:Lstart[-Lend]` to `https://github.com/$repo/blob/$sha/path#Lstart[-Lend]`.
- Put the primary evidence link(s) **on their own line** in the Smell/Defect block — GitHub renders the actual code snippet inline. Secondary `path:line` mentions in prose can stay inline links or plain text.
- Pin to `$sha`, never `main` — the evidence must show the code as it was when surveyed, not a line number that drifts.

Per surviving finding, `gh issue create` with: the skill's title prefix, `<ticket-type>` + the matching `area:*` / `platform:*` from `.github/labels.yml` (add `ready` **only** when the band is `now`), and the skill's Done-when body.
Then report the created tickets **and** the aggregated SOUND / rejected list, so the run is auditable.

## Guardrails
- One module per run. Read-only — never edit code; the tickets are the output.
- **Re-running is convergent:** dedup means a second pass adds only what the first missed. Completeness comes over a couple of runs, not one perfect pass.
- The behavior boundary (may a fix change behavior?) belongs to the calling skill, not this engine.
