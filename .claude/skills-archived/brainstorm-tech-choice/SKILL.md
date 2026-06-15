---
name: brainstorm-tech-choice
description: |
  Fires when user says "should we use", "X vs Y", "which library/framework/database/tool", "evaluate",
  "compare options", "what database/auth/stack", "let's pick a", "thinking about using". Walks problem
  framing → option set → criteria → trade-off matrix → recommendation, with mandatory vault lookup +
  context7 MCP for current docs. Auto-hands-off to /adr-write on decision.
when_to_use: |
  Use whenever weighing a technology/library/pattern choice for this project. Use BEFORE adding a new
  dependency. Use BEFORE picking auth/database/queue/etc.
---

# Brainstorm tech choice

## Mandatory first step (vault lookup)

Query the software-architecture-design vault MCP:
1. `mcp__software-architecture-design__vk_search "<topic>"` (e.g., "authentication", "database choice", "message queue")
2. `mcp__software-architecture-design__vk_get_note` on top 3 hits
3. If a relevant pattern/topic note exists, the option set + criteria should be informed by it

Also consult context7 MCP for current docs on candidate libraries:
- `mcp__plugin_context7_context7__resolve-library-id`
- `mcp__plugin_context7_context7__query-docs`

## The flow

### 1. Frame the problem
- What are we choosing for? (1–2 sentences)
- What feature/spec drives this? (link `docs/specs/<feat>/spec.md` if active)
- What constraints exist? (cost, hosting, language, team size — for solo: maintenance burden weight is high)

### 2. Surface the option set
2–4 realistic candidates. Avoid the "everything that comes up on Google" trap.
For each:
- 1-line description
- license + maturity (stars/recency/release cadence)
- whether it's covered in the vault (cite if yes)

### 3. Decision criteria
Derive from `docs/architecture.md` quality attributes + project-specific weights. Solo-project defaults:
- **Maintenance burden** (weight 3) — how much keeping-current does this cost me?
- **Documentation quality** (weight 3) — can Claude find current docs easily?
- **Integration with existing stack** (weight 2)
- **Lock-in risk** (weight 2) — how hard to swap later?
- **Performance** (weight 1, unless the spec demands)
- **Cost** (weight 2)

User can adjust weights for this specific choice.

### 4. Trade-off matrix

```
| Criterion (weight)        | Option A | Option B | Option C |
|---------------------------|----------|----------|----------|
| Maintenance burden (3)    | 3 ✓✓    | 4 ✓✓✓   | 2 ✓     |
| Documentation (3)         | 4        | 5        | 3        |
| Integration (2)           | 4        | 3        | 5        |
| Lock-in risk (2)          | 3        | 2        | 4        |
| Cost (2)                  | 5        | 4        | 3        |
| **Weighted total**        | **41**   | **41**   | **38**   |
```

Numerical totals are a starting point, not the verdict.

### 5. Recommendation
Name the lean choice + the **named trade-off you must accept**. No vague hedging.

Example:
> Recommend **Option A**. You're trading lower performance for a much smaller maintenance footprint, which matches your solo + production-grade goal. If perf becomes an issue, the swap to Option B is contained to the persistence adapter.

### 6. Save to docs/brainstorms/

Write `docs/brainstorms/YYYY-MM-DD-<topic>.md` with the full analysis. Files in `docs/brainstorms/` auto-prune at 30 days untouched (graduation to spec or ADR resets the timer).

### 7. Hand off to /adr-write on approval

When the user accepts the recommendation, auto-fire `/adr-write` and pass it the brainstorm doc + decision. The ADR pulls forward the decision + rationale.

If the user rejects or wants more options, iterate in the same brainstorm doc.

## EnterPlanMode for big choices

For decisions that affect 3+ modules or introduce a new external dependency: `EnterPlanMode` after step 5. User sees the recommendation as a plan, can reject/revise/approve. Approval triggers ADR + commit.

## Anti-patterns

- Choosing from a "list of popular libraries" without vault grounding.
- Recommendations without a named trade-off ("Option A is just better").
- ADR'ing before reaching consensus (premature lock-in).
- Skipping the brainstorm doc (loses the reasoning behind the decision).
