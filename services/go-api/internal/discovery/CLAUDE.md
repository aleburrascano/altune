# Discovery context — local rules & status

Covers the **discovery** bounded context: the search pipeline (`service/`),
providers (`adapters/providers/`), and the `cmd/discoveryeval` eval harnesses.
For how to build/run the API see `services/go-api/CLAUDE.md`. Pipeline shape +
ranking key: `ARCHITECTURE.md` (read it before auditing the pipeline).

## Current status (2026-06-24)

Pipeline is the rebuilt **Merge → Rank** core (ADR-0007 strangler addendum).
Correctness is solid; there is no known ranking bug. Measured against cloned
prod data (1897 tracks) via `discoveryeval`:

- **Exact `"artist title"` eval — 100% top-3.** This is the real product bar
  (what users actually type) and it's met.
- **Bare single-token title eval — ~81% top-3.** A stress metric with an inherent
  ambiguity ceiling: a bare title ("Hello", "Scorpion") legitimately surfaces the
  *famous* track over the user's niche owned one, and discovery is not
  personalized. Not a bug.
- **Merge — 0% under-merge / 0% over-merge.** Collapses everything it provably can.
- **Correction — 93% recall / 100% precision.**

**Popularity is currently NOT a live ranking signal.** `SearchResult.Popularity`
is never populated in the search path (the rebuild dropped the wiring), so it
stays 0 and ranking ties break on **multi-source → RRF**, not popularity. Wiring
it back (log of Deezer rank/fans, SoundCloud plays, Last.fm listeners) was tried
and **reverted (2026-06-24)**: a same-sample A/B showed it *regressed* the
bare-title eval (top-3 81%→75%) because this is a personal niche library — see
`docs/plans/2026-06-24-001-test-discovery-eval-harness-program-plan.md`. Don't
redo it naively. (The "Popularity > multi-source" line in old docs is stale
intent, not current reality.)

## Eval harnesses — the regression gate (`cmd/discoveryeval`)

Real pipeline, in-process, live providers + DB/Redis. Nightly/on-demand, **not**
per-commit. Every gated mode: run → gate metrics vs committed `baselines.json` →
print attributed-failure slices → exit 2 on regression.

```bash
cd services/go-api
go run ./cmd/discoveryeval -mode eval                 # ranking, exact corpus (top-3 bar)
go run ./cmd/discoveryeval -mode eval -corpus hard    # bare single-token titles (the hard case)
go run ./cmd/discoveryeval -mode merge                # under/over-merge
go run ./cmd/discoveryeval -mode correction           # synthetic-typo recall/precision (offline)
go run ./cmd/discoveryeval -mode diversity            # reshaping cost (rule on vs off)
go run ./cmd/discoveryeval -mode signal-a|signal-b    # coverage gaps / provider imbalance
go run ./cmd/discoveryeval -mode health|consensus     # report-only gauges (never gate)
go run ./cmd/discoveryeval -mode artwork -limit N -random  # artwork coverage: % of library artists resolving identity/provider/name/blank + attributed gaps (flush Redis first for a cold read)
# re-baseline (explicit, reviewed): measures value + empirical noise margin
go run ./cmd/discoveryeval -mode eval -update-baselines -noise-runs 3
```

Useful flags: `-limit N` (cap corpus), `-concurrency N`, `-top-k 3`,
`-query "X"` (dump top results for one query), `-json path`.

## Testing discipline (load-bearing)

- **Position, not presence.** "Is the right answer in the top 10" is far too weak.
  Gate is top-3 via `discoveryeval`; the canonical spot-checks below assert top-3.
- **A/B on an IDENTICAL deterministic sample** (`-limit`, no `-random`). Deltas
  across random samples are noise — a plausible change once looked "+7pp" that the
  same-sample A/B revealed as a *regression*. This is how the popularity attempt
  was caught.
- **No hardcoded workarounds.** If a query ranks wrong, fix the algorithm — never
  add a word to a bank or special-case list. They rot immediately.
- **Question stages before adding them.** Each session tends to add ranking layers.
  Ask: "if I remove this stage, do the positioning tests still pass?"
- **Log the math.** `LOG_LEVEL=debug` → `search.ranking` logs show per-result
  scores. **Verify provider responses directly** (`curl api.deezer.com/...`) — don't
  assume from memory.

### Canonical spot-checks (top-3, blended)

```
"Humble"              → top-3 contains the Kendrick track "HUMBLE."
"Scorpion"            → top-3 contains a Drake "Scorpion" result
"Bohemian Rhapsody"   → top-3 contains a Queen result
"Drake" / "Bad Bunny" → top-3 contains the artist
"Blinding Lights"     → top-3 contains the Weeknd track
"Kendrick Lamar Humble" → top-3 contains "HUMBLE." by "Kendrick Lamar"
```

Test blended AND filtered (`kinds=album`, `kinds=track`). `track>album>artist` is
a held-in-reserve, non-query-fit tiebreak for strict-#1 polish — not the gate.

## Known pipeline reality (current rebuilt core)

- **Rank order:** continuous relevance (token-sort similarity) → popularity
  (currently inert, see above) → multi-source → RRF (k=60) → stable title tiebreak.
  No relevance bands, no kind tiers, no intent contract (those were query-fit and
  were purged in the rebuild).
- **Merge:** identifier (ISRC/MBID) → exact canonical title+subtitle → cross-provider
  identity bridge. No fuzzy threshold. Only Deezer/MusicBrainz carry identifiers;
  most cross-provider pairs merge by exact canonical title.
- **Albums have no provider popularity data** (Deezer returns `nb_fan=0`); they
  compete on multi-source/RRF only.
- **Pre-correction disabled** (it rewrote valid queries from vocabulary pollution);
  post-correction (zero-results-only) is sufficient.

## Key files

- `ARCHITECTURE.md` — flow diagram + ranking key
- `service/search.go` — `Service` orchestrator (fanOut + mergeRankEnrich)
- `service/merge.go` / `service/rank.go` / `service/diversity.go` — the Merge→Rank→reshape core
- `service/enrich/` — detail-open enrichers (Deezer, Last.fm, Discogs, lyrics) + the `CachedLookup` read-through helper
- `service/eval/` + `cmd/discoveryeval/` — the offline regression harnesses (eval, merge, correction, diversity, health, coverage signals)
- `adapters/providers/` — one file per provider (Deezer, Last.fm, MusicBrainz, iTunes, SoundCloud, YouTube/YT Music, Discogs, Genius)

## Knowledge base

`okf/backend/discovery/index.md` indexes the nine discovery subsystem concept docs — read the relevant one before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
