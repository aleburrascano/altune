# test-hardening — programme plan

ADR: [docs/adr/0020-test-taxonomy-and-ratchet-gates.md](../../adr/0020-test-taxonomy-and-ratchet-gates.md)
Taxonomy: [okf/playbooks/test-taxonomy.md](../../../okf/playbooks/test-taxonomy.md)
Executor: [.claude/skills/qa-slice/SKILL.md](../../../.claude/skills/qa-slice/SKILL.md)
Selections: [okf/testing/](../../../okf/testing/)

> Rebuild the mobile test suite slice by slice against the twenty-category taxonomy, one slice per run of `/qa-slice`, each finishing with a committed selection record, a raised coverage floor, and a mutation score. **Read the ADR first** — it carries why the programme exists, and the alternatives that were rejected and should not be re-proposed. This file is the ledger: what is done, what is next, and what is outstanding.

## The loop, per slice

`/qa-slice apps/mobile/src/<path>/` runs it. In short: select all twenty categories and commit the verdict *before* writing tests → dispatch blind authors in parallel, one per work unit → verify every finding they report against the source yourself → resolve each as a source fix or an `it.failing()` → run Stryker and `test-assassin` → raise the ratchets → update the nested `CLAUDE.md` and any `okf/` concept the run touched.

Three things that are easy to get wrong, all learned the hard way:

- **Blind means blind.** The one permitted `okf/` file is the taxonomy. A pre-filled selection record is for the orchestrator, never the authors.
- **Harvest before deleting anything.** Findings that live only in a chat transcript are lost at the end of the session. `okf/testing/shared-acquisition.md` exists because eight verified survivors nearly were.
- **Prove each new gate fails when it should.** Apply the mutation by hand, watch the intended test go red, revert. Passing proves nothing.

## Slice ledger

Order is bottom-up through the dependency graph — `shared/` before `features/`, since a feature slice that mocks an unconstrained reducer inherits its gaps — and within that, by where bugs have actually shipped. `fallow health` is a good tiebreaker: it ranks files by complexity × churn × coupling and flags "complex functions with no test-coverage path".

| # | slice | status | kill rate | coverage floor |
|---|---|---|---|---|
| 1 | `shared/events` | **done** (2026-07-30) | 90.75% | 97/90/100/100 |
| 2 | `shared/acquisition` | **done** (2026-07-30) | 79.44%* | 97/88/100/100 |
| 3 | `shared/playback` | todo | — | 0 |
| 4 | `shared/offline` | todo | — | 0 |
| 5 | `shared/api-client` | todo | — | 0 |
| 6 | `shared/lib` | todo | — | 0 |
| 7 | `shared/telemetry` | todo | — | 0 |
| 8 | `shared/auth` | todo | — | 0 |
| 9 | `features/detail` | todo | — | 0 |
| 10 | `features/library` | todo | — | 0 |
| 11 | `features/playback` | todo | — | 0 |
| 12 | `features/discover` | todo | — | 0 |
| 13 | `features/auth` | todo | — | 0 |
| 14 | `features/settings` | todo | — | 0 |
| 15 | `shared/ui` | todo — Accessibility + Liveness only, deliberately | — | 0 |

`shared/ui` is 1,660 lines of primitives and theme: the cheapest coverage in the repo and the least meaningful. It gets two categories, not twenty.

*Slice 2's kill rate is a scoped `stryker run` measurement, not a `stryker.config.json`-enforced gate — see Outstanding.

## Outstanding

- **`stryker.config.json` doesn't scale past one slice.** One `mutate` glob and one global `thresholds.break`, raise-only. `shared/acquisition` measures 79.44% (dragged down almost entirely by `StyleSheet`/`Animated`-config mutations in its two presentational components, which this repo's own convention says not to chase); folding it into the same glob as `shared/events` (90.75%) would pull the *combined* score under the committed 90 threshold, and lowering the threshold to fit is exactly what raise-only forbids. Left `shared/events`-only for now. Every future slice with a UI component will hit this same ceiling — needs either per-slice Stryker configs/CI jobs or a different thresholding strategy before slice 9 (`features/detail`, the first UI-heavy feature slice) gets here.
- **Device smoke of the event stream.** The slice-1 SSE fixes (CRLF framing, retry-on-null-token, error logging) and the playlist cache-consistency fixes are verified only by tests written in the same session. Sign in, save a track, watch the row flip, background and foreground, confirm reconnect.
- **Re-measure the complexity ceiling from CI.** The step in `test-mobile.yml` is `continue-on-error` because a locally-derived 137 scored 145 on a depth-1 clone. With `fetch-depth: 0` in place, read the number from a green run, set `MAX`, drop `continue-on-error`.
- **Two gates never executed.** `deploy-backend`'s test dependency needs a `services/go-api/**` push; `release-ios`'s needs a tag. Both are wired and unproven.
- **`react-doctor.yml` stays advisory** and should not be made blocking — it runs `@latest`, and a gate must not change behaviour because upstream shipped a rule. The pinned gate lives in `test-mobile.yml`.
- **`Discovery eval nightly` is red**, pre-existing and unrelated.
- **The Go suite's kill rate is unmeasured.** 268 test files behind a 65% coverage floor — the same profile the mobile suite had when it measured 38%. Two `test-assassin` runs on `internal/acquisition/service` and `internal/catalog/service` would establish whether the backend half of this programme is needed at all. Deliberately deferred; worth doing purely to find out there is no work.

## Done

- Taxonomy, selection-record format, and `qa-slice` rewired to drive from it.
- Harness rebuilt: doubles in `apps/mobile/jest/doubles/` that round-trip writes through reads and take injected failures, plus `__tests__/harness.test.ts` constraining the doubles themselves.
- Per-glob coverage floors; Stryker (`npm run mutate`) with a raise-only score floor; nightly `mutation-mobile.yml`.
- `test-mobile.yml` gating typecheck, lint, fallow dead-code/dupes, coverage, and a pinned react-doctor error gate — previously nothing in CI ran the mobile suite.
- `deploy-backend` and `release-ios` now consume their test workflows via `workflow_call` + `needs`.
- `.fallowrc.json` boundary zones corrected to match `apps/mobile/CLAUDE.md`; they had permitted three cross-feature edges and `shared -> feature-auth`.
- Slice 1: nine defects fixed, 186 tests, 0% → 90.75%.
- Slice 2: three defects fixed (a NUL-byte identity-key separator, a banned "songs" noun, a store `remove()` that would have wiped every other track), 106 tests, 0% → 98.83%.
