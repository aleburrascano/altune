---
name: refactoring-with-patterns
description: Audits one feature of one codebase and writes a ranked STRUCTURE-AUDIT report - up to ten detailed findings plus unlimited deferred one-liners, each with concrete line citations, a named fix (or "no pattern - direct code"), what varies, and what tracing costs. Requires a Considered and rejected section. Runs autonomously and is safe to re-run; the report is the review artifact. Use when the user says refactor, restructure, audit, clean up, "this is a mess", names a feature to review, or asks about coupling, boundaries, dependency direction, or applying design patterns. Do not use for bug fixes, new features, or general code review.
---

# Refactoring with patterns

Audit **one feature of one codebase** — `playback` in the backend, not the whole
backend. Write the report to the OS temp directory, not the repo — it's a
throwaway review artifact, not a checked-in file:

```
<tmp>/structure-audits/<feature>/STRUCTURE-AUDIT-<feature>-<timestamp>.md
```

`<tmp>` is `$TEMP` (Windows) or `$TMPDIR`/`/tmp` (elsewhere). `<timestamp>` is
`YYYYMMDD-HHMMSS`, so re-running the same feature the same day never overwrites
the previous report. Create the folder if it doesn't exist. Run to completion
without checking in; the report is where review happens.

Ask which feature if it isn't named. Never audit two at once.

## The three rules

**Start from the code, not the manifest.** Find where it hurts, then name the fix.
The manifest is vocabulary for a fix already justified on its own terms. Reading a
catalog produces catalog-shaped findings. But the inverse failure is asserting
"no pattern — direct code" without ever checking: after drafting, every verdict
is reconciled against the lexicon (step 6) — an unchecked "no pattern" is as
invented as an unresolved citation.

**Ten detailed findings maximum — but nothing gets dropped.** The cap is a detail
budget, not an attention budget. Rank by concrete pain; findings 11+ become
one-liners under **Deferred**. They surface again on the next run.

**A feature that can't be located is the first finding.** Before reading anything,
establish the feature's boundary. If it spans six packages and can't be extracted,
that is the audit's most important result and everything else is downstream of it.

## Workflow

```
Audit Progress:
- [ ] 1. Locate the feature boundary
- [ ] 2. Survey structure at that boundary (no file contents)
- [ ] 3. Read only what the boundary contains
- [ ] 4. Draft findings, ranked, ten detailed + deferred one-liners
- [ ] 5. Write Considered and rejected
- [ ] 6. Reconcile every verdict against the lexicon (manifests + targeted entry reads)
- [ ] 7. Run verify_citations.py, fix, repeat until clean
- [ ] 8. Write the report
```

### 1-2. Locate and survey

Survey tooling, no file contents yet: [reference/method.md](reference/method.md)
— detect the ecosystem, then answer, in any language, **how many directories
contain this feature's code, what does the feature import, and what imports it?**

- One or two packages, imports point outward-in → the feature has a boundary.
  Audit inside it.
- Five-plus packages, or imported by unrelated features → it has no boundary.
  That's F1, and the fix is structural, not a pattern.

### 3. Read

Only what's inside the boundary. Prefer symbol-level navigation over whole files.
If the boundary is wide, read the fan-in points first — that's where the coupling
is.

### 4-5. Draft and reject

Draft from the code alone — the manifest stays closed until every finding and
rejection exists. That ordering is what keeps findings code-shaped.

### 6. Reconcile against the lexicon

Now — and only now — open the lexicon (`~/.claude/lexicon/`):

1. Read `INDEX.md`, then the audited language's manifest (`MANIFEST-<lang>.md`)
   and `MANIFEST-universal.md`. If the findings touch a
   cross-cutting domain with its own manifest (caching, event-driven,
   observability, microservice boundaries…), read that one too.

   In that manifest, the **anti-pattern / code-smell / refactoring entries** are
   where most findings anchor, because most fixes are "no pattern — direct code":
   a named smell (dead code, duplicated code, god object, long function,
   primitive obsession) with its own cost line fits a direct fix better than the
   closest runtime pattern forced to stand in. Every language mirror carries
   these entries; the section name varies, so scan the manifest you just read for
   `smell`, `anti-pattern`, or `refactor` and open those before the pattern
   catalog. The pattern catalog is for the minority where a runtime pattern
   genuinely applies.
2. Anchor **every** finding to the entry it is actually about:
   - a fix that **removes a smell** (dead code, duplication, a god object, a
     long function, a magic constant) → name that smell from the code-smell
     catalog. The fix IS its remedy; its cost line is the "when NOT to refactor"
     caveat.
   - a fix that **applies a runtime pattern** (Facade, Circuit Breaker, Cache
     Key Design) → the pattern's entry.
   - only when neither fits → the **closest** entry and why it loses.
   Cite entries as `` `lexicon:go/…/strategy-pattern` `` (the manifest gives the
   path).
3. For every cited entry, open the full entry at
   `~/.claude/lexicon/site/<path>/index.html` and quote its cost text verbatim
   into the finding's **Cost:** line. Entries run ~40k chars — never read one
   whole; Grep the HTML for `Avoid|Cost|when` and read that window. The
   verifier checks the quote actually appears in the entry, so paraphrase fails.
4. Rejections get the same treatment: `lexicon:` path + quoted cost line (the
   cost line usually IS the rejection). A rejection with no adjacent entry says
   "no manifest entry" instead.
5. Reconciliation must not mint findings. If a manifest read reveals something
   real the code pass missed, it goes under **Deferred** for the next run —
   that keeps rule 1 honest. The smell catalog especially reads like a checklist;
   resist grading the feature against every smell. Anchor the findings the code
   already produced, and Defer anything new the catalog surfaces.

A cost line quoted from the entry is the point of the whole lexicon: it states
when NOT to use the pattern. An entry without one is unproven, not free.

### 7. Verify

```bash
python3 verify_citations.py <tmp>/structure-audits/<feature>/STRUCTURE-AUDIT-<feature>-<timestamp>.md
```

Every `file:line` must resolve; every finding must carry a resolving `lexicon:`
citation and a cost quote that appears verbatim in the cited entry; every
rejection must cite an entry or say "no manifest entry". Fix and re-run until
clean. A finding whose citation doesn't resolve — file or lexicon — is a
finding that was invented.

## Report format

````markdown
# Structure audit — <feature> (<backend|frontend>)

<N> files · <M> packages · <date> · short commit sha if available

## Boundary

Where this feature lives, what it imports, what imports it. One paragraph.
State plainly whether it could be lifted out without breaking callers.

## Findings

### F1. <short title>

- **Severity:** high | medium | low
- **Evidence:** `playback/service.go:42`, `playback/service.go:12`
- **Pain:** what is concretely hard or broken now. Not "this is coupled."
- **Fix:** <pattern> (`lexicon:go/…`) | no pattern — direct code
  (smell it removes, or closest entry: <entry> (`lexicon:go/…`) — why it applies / loses)
- **Cost:** "<verbatim quote from the cited entry's cost/avoid-when text>"
- **What varies:** the concrete second implementation or future change.
  "Nothing — this is a direct fix" is a valid and common answer.
- **Tracing cost:** what go-to-definition must do after this change
- **Effort:** S | M | L
- **Depends on:** none | F3

### F2. ...

## Deferred

Real but lower-ranked. One line each, no cap. Not dismissed — next run's findings.

- `playback/queue.go:88` — three near-identical shuffle helpers, no shared path.
- `playback/cache.go:14` — package-level mutable state, unclear owner.

## Considered and rejected

- **Repository over `storage/`** (`lexicon:go/domain-driven-design-ddd-patterns-in-go/repositories`) —
  one database, no second planned. Its own cost line rules it out: "Skip or
  delay a repository when: the service is a small CRUD API with little domain
  behavior."
- **Strategy for the shuffle modes** (`lexicon:go/behavioral-patterns-in-go/strategy-pattern`) —
  three modes, one file, none swapped at runtime. "Avoid Strategy when: there
  is only one algorithm and no realistic alternative."
- **Splitting the queue into a subpackage** — no manifest entry; rejected on
  tracing cost alone.

## Not found

Checked for and absent. Keeps the next run from re-litigating.
````

## Finding discipline

**Required on every detailed finding:**

1. Fix names a manifest pattern with its `lexicon:` path, or — for "no pattern —
   direct code" — names the smell it removes (from the code-smell catalog) or the
   closest entry it beats, with the path. Either way the finding quotes the cited
   entry's cost / when-not-to text — verified, so it must come from the actual
   entry, not from memory
2. What varies names something concrete. "Flexibility" and "testability" are not
   answers — they describe wanting to do it, not a reason to
3. Tracing cost is stated

If "what varies" is speculative and tracing gets worse, it is not a finding. It
goes in **Considered and rejected**.

**The rejected section is not optional.** An audit that rejected nothing did not
discriminate — it collected. Expect it to be longer than the findings list.

**"Delete the abstraction" is a finding.** Interfaces with one implementation,
factories with one product, wrappers that only forward. These accumulate fast in
maintained code because each looked reasonable in isolation and nothing removes
them. Adding structure reads as contribution; removing it reads as doing less.
Correct for that.

### Examples

**Not a finding — reads as one, justifies nothing:**

> PlaybackService is tightly coupled to the S3 client. Introduce a Storage
> interface and inject it. Decouples storage and improves testability.

No second implementation. "Decoupled" and "testable" are vocabulary, not reasons.
Tracing cost unstated. → Considered and rejected.

**A finding — the boundary problem:**

> ### F1. Playback has no boundary
> - **Severity:** high
> - **Evidence:** `handlers/playback.go:1`, `services/player.go:1`,
>   `repositories/tracks.go:1`, `common/audio.go:1`
> - **Pain:** playback code lives in four packages, none named for it. Changing
>   shuffle behaviour touches three directories. Nothing can load the feature into
>   context without loading everything it's smeared across, and `common/audio.go`
>   is imported by six unrelated features, so any change there risks all of them.
> - **Fix:** no pattern — direct code. Create `playback/`, move the four files in,
>   split `common/audio.go` so only the playback half moves.
>   (closest: Go Package Organization (`lexicon:go/idiomatic-go-design-patterns/package-organization-patterns`)
>   — this fix IS that entry's advice; no runtime pattern applies.)
> - **Cost:** "Too many packages add friction: Excessive splitting creates
>   navigation overhead."
> - **What varies:** nothing. This is a direct fix.
> - **Tracing cost:** improves — one package instead of four.
> - **Effort:** L
> - **Depends on:** none

**A finding — accepts a pattern, prices it:**

> ### F4. Unbounded retries against the transcoder
> - **Severity:** medium
> - **Evidence:** `playback/transcode.go:88`
> - **Pain:** every stream start retries the transcoder three times with no
>   backoff. Under partial outage this amplifies load on the failing service and
>   holds goroutines until the pool exhausts, taking playback down with it.
> - **Fix:** Circuit Breaker (`lexicon:go/concurrency-design-patterns-in-go/circuit-breaker`)
> - **Cost:** "State and tuning complexity: thresholds, windows, and half-open
>   behavior need real-world calibration."
> - **What varies:** nothing structural — this is about failure mode. Concretely:
>   playback fails fast with a cached-stream fallback instead of hanging.
> - **Tracing cost:** one hop, `stream → breaker.Do → transcoder.Start`. Concrete
>   struct, not an interface, so go-to-definition still resolves to one target.
> - **Effort:** M
> - **Depends on:** none

## Structural fixes outrank patterns

Most real messes are boundary and dependency-direction problems. No manifest
pattern fixes those, and they matter more than any pattern for making a feature
loadable in isolation:

- **A feature is a directory, not a grep.** Behavior lives with its data:
  `playback/`, not `handlers/` + `services/` + `repositories/`. Layer-first layout
  means every change touches five directories and nothing can be read alone.
- **Shared packages are where boundaries go to die.** `common/`, `utils/`,
  `shared/` imported by everything means no feature can be extracted. Splitting
  them is usually worth more than any pattern in the manifest.
- **Imports point one direction, enforced by lint.** A config that fails CI beats
  discipline. Linter setup per ecosystem is in [reference/method.md](reference/method.md)
  (step 4). If the audit finds a direction problem, propose the rule alongside the
  fix — otherwise it regresses on the next feature.
- **Object graph constructed explicitly in main.** No reflection container. When
  main shows the whole graph, go-to-definition traverses reality and resolves to
  one candidate instead of six.

Patterns replace static references with runtime resolution — that is their
mechanism, and it is what makes code hard to trace without executing it. In a
codebase navigated by grep and go-to-definition, indirection is the tax. Spend it
only where something real varies.

## Out of scope

Bug fixes, features, dependency upgrades, test writing. Note and move on.
