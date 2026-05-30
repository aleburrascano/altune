# Streaming-platform search vs. altune — Apple Music, SoundCloud, Deezer, Tidal

> Status: brainstorm (research). Created 2026-05-29. Companion to [spotify-search-comparison.md](./spotify-search-comparison.md). Auto-prunes at 30 days untouched unless graduated to a spec/ADR.

**Scope.** Three of these four — **Deezer, Apple Music (via the iTunes Search API), SoundCloud** — are altune's *own providers*, so their search behaviour *is* the quality of candidates altune receives. Tidal is **not** an altune provider; the question there is "worth adding as a 7th?". This compares each platform's *publicly documented* search behaviour against altune's federation engine and proposes harness-measurable changes.

**Hard constraint honoured throughout.** These platforms disclose even less about search internals than Spotify. Every platform-specific claim carries a public-source citation or is labelled `[INFERRED]`/`[ASSUMED]`/`[OPEN]`. Several intended sections (the *modern* Apple Music API; all of Tidal) **could not be verified** and are flagged `[OPEN]` rather than asserted.

**altune baseline (the "today" reference).** Metadata-federation: fan-out to 6 provider APIs → merge/dedup (ISRC/MBID/Jaro-Winkler) → `fuse_and_rank` (relevance band → demotion → bootleg → popularity → RRF → multi-source → **per-source prior** → alpha). Per-source priors today are hand-set: MusicBrainz 0.95, Deezer 0.85, iTunes 0.85, Last.fm 0.80, TheAudioDB 0.78, SoundCloud 0.65 [VERIFIED:Read@services/api/src/altune/application/discovery/dedup.py#L36-L43].

---

## Executive summary

The single most important finding: **none of altune's three consuming providers publishes a documented relevance sort**, and the iTunes API documents **no typo/fuzzy matching at all**. This *validates* altune's core design — owning relevance ranking in `fuse_and_rank` and owning typo tolerance via rapidfuzz is not redundant, it's necessary, because the providers don't do it for us. The corollary for the per-source priors: the public docs justify **no specific prior values** in either direction — re-tuning them must be **empirical** (driven by `scripts/discovery_eval/` per-provider hit@1/coverage), not derived from documentation. The two genuinely new opportunities are both `[OPEN]` (unverified by this research and needing a direct API-doc check): the **modern Apple Music API** (distinct from the legacy iTunes Search API altune uses — may expose relevance order + autocomplete) and **Tidal** as a 7th provider.

---

## Deezer (provider · prior 0.85)

- **Default order is opaque popularity ("RANKING"), not a relevance score.** The only control is an `order=` parameter taking predefined field-sort values; the default is `RANKING` (popularity/best-match). Deezer FAQ: *"You just have to add the parameter "order=" at the end of your request and set a value defined in this list"* [VERIFIED:WebFetch@https://support.deezer.com/hc/en-gb/articles/360011538897-Deezer-FAQs-For-Developers] (value list e.g. `RANKING`, `RATING_ASC/DESC`, `DURATION_DESC`, per the developer docs). `order=` is a field-sort selector, **not** a relevance-score sort.
- **No precision guarantee — Deezer openly admits false positives.** *"We are trying our best to provide you with the most accurate results. Our search engine is continuously improving, but occasionally there may be some false positives."* [VERIFIED:WebFetch@https://support.deezer.com/hc/en-gb/articles/360011538897-Deezer-FAQs-For-Developers]
- **altune relevance:** Deezer is a high-recall, popularity-ordered, best-effort source. It returns ISRCs (enabling high-confidence merges) and is our most reliable provider in the eval runs (336/336 OK). Treat as useful-but-imperfect — its 0.85 prior is reasonable but not doc-grounded. Typo/fuzzy behaviour of the endpoint is **undisclosed** `[INFERRED]`.

## Apple Music / iTunes Search API (provider · prior 0.85)

- **The legacy iTunes Search API altune uses exposes NO sort/relevance parameter and NO typo tolerance.** Exactly 9 documented params — `term, country, media, entity, callback, limit, lang, version, explicit` — and *"There are NO parameters documented for sort order, relevance, ranking, spell-correction, or fuzzy/typo matching … The API documentation is silent on these topics entirely."* [VERIFIED:WebFetch@https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/Searching.html] The `entity` param selects result *type*, not order.
- **altune relevance:** iTunes gives clean catalogue metadata but **cannot** be relied on for misspelling correction or relevance ordering — this directly **validates altune's rapidfuzz `token_sort_ratio` layer**. (It's also the flakiest provider under batch load — heavy rate-limit/timeout in eval runs — orthogonal to relevance.)
- `[OPEN]` **The *modern* Apple Music API (`developer.apple.com/documentation/applemusicapi`) is a different, richer API** than the legacy iTunes Search API altune consumes. Claims that it exposes a relevance order (`meta.results.order`), a `search/suggestions`, and a `search/hints` autocomplete endpoint were **refuted or unvoted** in this research — so they are *not established here*. Worth a direct doc check: if true, switching altune's Apple provider could improve candidate quality *and* unlock autocomplete (see Spotify report §3). It needs a developer token (JWT) vs the keyless iTunes API — a real cost.

## SoundCloud (provider · prior 0.65)

- **Official API: single free-text `q`, recency-ordered, no documented relevance.** *"Search tracks, users, and playlists using the `q` parameter, which matches against fields like `title`, `username`, and `description`."* [deep-research verified 3-0, developers.soundcloud.com/docs/api/guide]. The only popularity sort ("hotness") was removed: the `/tracks` endpoint *"will ignore the `order` parameter and default to ordering by creation date"* [VERIFIED:WebFetch@https://developers.soundcloud.com/blog/removing-hotness-param/] — i.e. **recency, not relevance**.
- **No search-specific rate limits or ranking algorithm documented** [deep-research verified 3-0]; no ISRC/canonicalization guarantees.
- **altune relevance — important nuance:** altune does **not** use this official API; it ingests SoundCloud via **yt-dlp `scsearch`**, so the above characterises the *platform*, adjacent to our real path. SoundCloud is the lowest-trust source (no relevance, recency-ordered, metadata-thin, bootleg-heavy) — its 0.65 prior is *consistent* with that, but justified empirically, not by docs. `[OPEN]` whether `scsearch` ordering differs from the official-API recency default.

## Tidal (NOT a provider)

- `[OPEN]` **Entirely unverified by this research.** A developer portal exists (`developer.tidal.com`) but every Tidal claim failed verification — whether it offers a usable public *search* API, documented relevance, or ISRC behaviour is **unknown**. The "should Tidal be a 7th provider" question is **unanswered**; it needs a direct spike against the official API docs before any decision. Do not treat as recommended.

---

## Cross-platform summary

| platform | altune provider? | query understanding / typo | ranking signal (documented) | autocomplete | architecture | altune takeaway |
|---|---|---|---|---|---|---|
| Deezer | ✅ (0.85) | undisclosed `[INFERRED]` | default popularity ("RANKING"); admits false positives | undisclosed | undisclosed | high-recall, ISRC, best-effort |
| Apple / iTunes API | ✅ (0.85) | **none documented** | **none documented** (9 params) | none (legacy API) | undisclosed | clean metadata; we must own relevance+typo |
| modern Apple Music API | ❌ `[OPEN]` | `[OPEN]` | `[OPEN]` (meta.order?) | `[OPEN]` (hints?) | `[OPEN]` | possible provider upgrade — verify first |
| SoundCloud | ✅ (0.65) via yt-dlp | none documented | **recency, not relevance** | none | (Elasticsearch — `[OPEN]`) | lowest trust; metadata-thin |
| Tidal | ❌ `[OPEN]` | `[OPEN]` | `[OPEN]` | `[OPEN]` | `[OPEN]` | unverified — spike before adding |

---

## Recommendations (ranked by impact × effort)

| # | Recommendation | Impact | Effort | Fit |
|---|---|---|---|---|
| 1 | **Empirically re-tune the `dedup.py` per-source priors from harness data, not from docs.** Add a per-provider attribution mode to `scripts/discovery_eval/` (it already records provider statuses) that measures each provider's marginal hit@1 contribution; set priors from that. The research proves the *current* values aren't doc-grounded — so measure them. | High | Low–Med | **Good** — pure tuning + measurement, no new dependency. |
| 2 | **Keep owning relevance + typo tolerance — it's validated.** iTunes documents zero fuzzy matching and no provider documents a relevance sort, so `fuse_and_rank` + rapidfuzz is load-bearing, not redundant. (Pairs with the Spotify report's pre-fan-out spell-correct recommendation.) | High | — | **Confirmed** — no change, just confidence. |
| 3 | **Spike the *modern* Apple Music API** (`developer.apple.com/documentation/applemusicapi`): confirm whether it exposes relevance order + `search/suggestions`/`search/hints`. If yes, evaluate swapping altune's Apple provider (cost: JWT auth) — could lift candidate quality and unlock autocomplete. A/B via the harness. | Med–High `[OPEN]` | Med | **Verify-first** — claims refuted here; needs a direct doc read. |
| 4 | **Spike Tidal's public API** for a usable search endpoint + ISRC; only add as a 7th provider if a harness A/B shows a hit@1/coverage lift. | Med `[OPEN]` | Med | **Verify-first** — entirely unverified. |
| 5 | **Re-examine SoundCloud's value.** It's recency-ordered, metadata-thin, ISRC-absent, and the flakiest to ingest (yt-dlp). Measure its marginal contribution in the harness; if near-zero, consider dropping it to cut latency/rate-limit pressure. | Low–Med | Low | **Good** — measurement-driven prune candidate. |

---

## Caveats

- **Verified evidence covers only Deezer, the legacy iTunes Search API, and SoundCloud's official API** — and only their *public documentation*, not internal ranking. 8 claims confirmed, 17 killed in adversarial verification.
- **The modern Apple Music API and all Tidal claims were refuted or unvoted** — every "Apple Music has relevance order / suggestions / hints" and every Tidal claim failed. They are `[OPEN]`, not findings. Do not act on them without a direct API-doc spike.
- **SoundCloud's richer engineering story** (DiscoRank PageRank-style ranking; Elasticsearch infra) appeared only in *unvoted* claims — cannot be asserted at confidence.
- **Deezer's order/default claim went 2-1** (one dissent) and conflates Deezer's own "popularity" label with "relevance" — treat the default as popularity-ish best-match, undisclosed internally.
- **No evidence supports re-tuning priors in a specific direction** — only that they aren't doc-grounded. Direction must come from the eval harness.

## Open questions

1. What do per-provider harness numbers (hit@1/coverage marginal contribution) actually say — should Deezer/iTunes 0.85 move relative to MusicBrainz 0.95, and is SoundCloud 0.65 too high given recency-only, ISRC-absent results?
2. Does the modern Apple Music API expose relevance order + autocomplete, and is the JWT-auth cost worth a provider swap?
3. Does Tidal have a usable public search API with ISRC, and does adding it lift coverage/hit@1?
4. Does yt-dlp `scsearch` ordering differ from SoundCloud's documented recency-default, changing SoundCloud's effective trust?
