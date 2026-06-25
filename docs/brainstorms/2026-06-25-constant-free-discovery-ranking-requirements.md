---
date: 2026-06-25
topic: constant-free-discovery-ranking
---

# Constant-Free Discovery Ranking

## Summary

Remove the single query-fit constant left in the live ranking path (the `0.35` distinguishing-token boost) and replace ad-hoc "token-sort relevance + IDF bonus" with one parameter-free relevance measure — IDF-weighted, continuous per-token fuzzy coverage over title+subtitle — whose exact published form is selected empirically by a newly **deterministic** eval gate. The win is a constant-free ranking path and a trustworthy gate, not a higher top-3 number.

---

## Problem Frame

Discovery's ranking was rebuilt (ADR-0007 strangler addendum) around published algorithms — token-sort similarity, RRF — explicitly to purge query-fit tuned constants. `rank.go`'s own header states the principle: "no tuned constants… a similarity measure is a published algorithm, not a fitted constant."

A prototype IDF "distinguishing-token boost" (`distinguishingBoostWeight = 0.35`) was then added to fix a narrow case ("Ken Carson Olympics", where the rare song token names the track). Tracing three eval failures with the `discoverytrace` tool showed the boost is a **net regression on the most common query shape there is** — "artist + song". When the artist name is rarer than the common-word title tokens, the boost rewards single-source junk uploads that stuff "Artist - Title" into their title field, and demotes the multi-provider canonical track (which correctly carries the artist in its subtitle). Concretely, for "Michael Jackson The Way You Make Me Feel" the canonical (16 sources) was buried at position 19 under 19 single-source junk uploads.

The `0.35` is also a fitted magic number — exactly the thing the rebuild purged — reintroduced as a weight rather than a word-list. And the eval that should have caught this is **non-deterministic**: it hits live providers, its committed baseline is an unrealistic `1.0000 ±0.0000`, and its failure *set churns* run to run, so real regressions hide in the noise. That churn is what made the boost's damage hard to see in the first place.

---

## Requirements

**Constant removal and relevance**
- R1. Remove `distinguishingBoostWeight` and the distinguishing-token boost from the live ranking path (`service/rank.go`), and delete its duplicated math in `cmd/discoverytrace`.
- R2. Replace the relevance computation with a single parameter-free measure: IDF-weighted, per-token fuzzy coverage of the query over the result's **title + subtitle**. IDF is the per-token weight (document frequency across the per-query candidate set); the per-token match is a fuzzy ratio. No bolt-on bonus.
- R3. The per-token match is **continuous** — the raw per-token Levenshtein ratio — with **no match cutoff/threshold** (a cutoff would be a new query-fit constant).
- R4. The exact published form of relevance is selected **empirically by the deterministic eval** from the parameter-free candidate set: {asymmetric IDF coverage, IDF-weighted Dice, asymmetric coverage + length tiebreak}. A blended `α·asymmetric + (1−α)·symmetric` hybrid is **prohibited** — the blend weight is the removed constant reborn.
- R5. Keep the multi-source provider-count ladder as the constant-free tiebreak beneath relevance (it is what discriminates canonical from junk once relevance ties).

**Deterministic eval gate**
- R6. Make the ranking eval deterministic by recording raw **provider HTTP responses** for the eval corpus into versioned fixtures and replaying them through the **real Merge→Rank pipeline** (inject at the provider-I/O boundary, not the `Searcher` result seam — the result seam already contains ranked output and cannot test a ranking change).
- R7. Re-baseline the eval against the fixtures, replacing the unrealistic `1.0000 ±0.0000` committed baseline with a deterministic fixture baseline.
- R8. Clean up the eval-**matcher** artifacts (`¥$` symbol-only artist, the CHERISH malformed-dup query, the Elvis live-version mismatch, `feat. <self>`) as part of the eval work, so the gate is not polluted by phantom failures.

**Guardrail**
- R9. (Structural) The live ranking path carries **zero query-fit / corpus-fitted constants**. Operational constants (timeouts, circuit-breaker threshold, ring sizes) and published-algorithm constants (`rrfK = 60`) are permitted; any number tuned to make specific queries pass is not.

---

## Acceptance Examples

- AE1. **Covers R2, R5.** Given query "Ken Carson Olympics" and a candidate whose title is "Olympics - Ken Carson, Lil Tecca" (uploader in the artist field), when ranked, the correct track appears in the top 3 — recovered without any boost, because covering the rare "olympics" token is the relevance score itself.
- AE2. **Covers R2, R5.** Given query "Michael Jackson The Way You Make Me Feel" with one multi-source canonical (artist in the subtitle) and many single-source junk uploads embedding the artist in the title, when ranked, the multi-source canonical is in the top 3, above the junk (relevance ties on title+subtitle coverage; the count-ladder breaks it for the canonical).
- AE3. **Covers R3, R4, R9.** Given the chosen relevance form, when inspected, it contains no scalar fitted to the corpus — no per-token match cutoff, no blend weight, no tuned bonus.
- AE4. **Covers R6.** Given the same recorded fixtures, when the eval runs twice, the per-query top-3 results and the headline metrics are byte-identical.

---

## Success Criteria

- The live ranking path contains zero query-fit constants — the boost is gone and no knob (match cutoff, blend weight) was reintroduced. Verifiable by inspection.
- The eval is deterministic: two runs on the same fixtures produce identical metrics and failure sets.
- No net top-3 regression on the deterministic exact corpus versus the fixture baseline; "Ken Carson Olympics" and the Lil Tecca queries are recovered (named spot-checks).
- The hard-corpus ~81% top-3 is explicitly treated as an ambiguity/personalization ceiling — not gated for improvement, not a failure of this work.
- Downstream handoff: a future engineer can A/B a ranking change against the fixtures and trust the delta, because the inputs are frozen and the gate is stable.

---

## Scope Boundaries

- **ML / learned ranker** — out. It is the maximal bag of corpus-fitted constants and directly contradicts the no-fitted-constants constraint. Roadmap-deferred (the Spotify "Which Witch" approach, revisit when the team grows). This work's deterministic fixtures are the prerequisite ML will need — it is the runway, not a competitor.
- **Personalization / niche-vs-famous disambiguation** — out. It is the only lever on the hard-corpus ceiling, and it is a separate scope.
- **Approach C (distribution-relative thresholds)** and **blended α-hybrid relevance** — out. Both risk smuggling a constant back in.
- **Re-wiring popularity as a ranking signal** — out. Tried and reverted 2026-06-24 (regressed the bare-title eval on this niche library).
- **Symbol-preserving normalization** — out. Keeping symbols re-breaks hyphen-glued titles (per the `normalize.go` AIDEV-NOTE); the `¥$` fix is matcher-side, not normalization-side.
- **Operational constants** (timeouts, circuit-breaker threshold, ring sizes) — untouched; they are not query-fit.
- **Raising the eval top-3 number** — not a goal. Exact is already 100%, hard is ceilinged; the deliverable is the removed constant and the trustworthy gate.

---

## Key Decisions

- **Relevance is one formula, not a blend of two scores.** Blending a fuzzy char-level score with an IDF token-level score needs a combining weight — the `0.35` reborn. Instead the fuzz lives *inside* each token's match (parameter-free Levenshtein ratio) and IDF is the *weight* (df-based, parameter-free). One score, no knob.
- **Computed over title+subtitle, not title-only.** Title-only was the boost's mistake: it rewarded artist-name-in-title junk. Over title+subtitle, a canonical with the artist in its subtitle covers the rare token just as well as the junk, so they tie on relevance and the count-ladder picks the canonical.
- **The relevance form is chosen by the eval, not by argument.** Asymmetric coverage, IDF-weighted Dice, and coverage+length-tiebreak are all parameter-free; which ranks best is empirical. This is the entire reason the deterministic eval is the prerequisite.
- **Determinism is injected at the provider-HTTP boundary.** The `Searcher` result seam already holds ranked output, so replaying it cannot test a ranking change. Only provider-level fixtures replayed through the real Merge→Rank exercise the code under test.
- **Matcher-artifact cleanup is bundled in.** A gate the design depends on must not emit phantom failures; cleaning them up protects the trustworthiness the whole plan rides on.
- **Definition of done is constant-free + no-regression + 2 recoveries + a trustworthy gate — not a higher number.** Exact is maxed and hard is ceilinged, so there is no headroom to "improve the number." "Improve all eval" is reframed as the ongoing gate-driven program the deterministic harness enables.

---

## Dependencies / Assumptions

- **Build order (prerequisite chain):** (1) deterministic eval fixtures + matcher cleanup → (2) prototype the candidate relevance forms behind `discoverytrace` → (3) swap relevance behind the now-trustworthy gate → (4) remove the boost and its `discoverytrace` duplication. The gate must exist before the swap can be validated.
- The `httptrace` recorder and `discoverytrace` (built 2026-06-25) already capture raw provider JSON — fixtures reuse existing infrastructure, not new machinery.
- Correction fires only on zero results (`search.go:168`), so ranking receives typo'd-but-nonempty queries; per-token fuzz is retained for that real-world case even though the eval corpus itself is clean.
- Provider catalogs drift; fixtures need periodic re-recording. This faithfulness cost is accepted — drift ages the absolute numbers but does not harm A/B validity (same frozen inputs, compared orderings).

---

## Outstanding Questions

### Deferred to Planning

- [Affects R4][Technical] Which parameter-free relevance form wins on the deterministic exact corpus? Empirical — the eval decides once fixtures exist.
- [Affects R6][Technical] Exact seam for recording/replaying provider HTTP fixtures (per-provider `RoundTripper` injection via the `httptrace` recorder vs. another boundary), and how fixtures key to corpus queries.
- [Affects R2, R3][Needs research] Does continuous per-token fuzzy matching introduce ordering noise that IDF weighting + the count-ladder don't absorb on the exact corpus? If so, the parameter-free structural fix is exact-token-match scores 1.0, fuzzy only as fallback — never a threshold. Measured on fixtures.
- [Affects R8][Technical] Exact matcher fix for symbol-only / degenerate ground truth (`¥$`, CHERISH dup, Elvis live, `feat. <self>`) — how the eval compares expected vs. returned when the normalized expected form is empty or malformed.
