# Discovery search — empirical trend audit

Created 2026-06-27. Status: living findings doc. Method: ran a diverse battery of
real queries through `cmd/discoverytrace` (real providers → Merge → Rank →
reshape, read-only) and clustered the **failure signatures**. The data — not a
whiteboard — drives the priority order. Companion to
`2026-06-27-discovery-quality-program-requirements.md`, which it partially
**corrects** (see "Plan corrections").

## Tool caveat (applies to every finding)
`discoverytrace` intentionally **skips the identity bridge (cross-provider id
merge) and artwork enrichment** — its header says so. So cross-provider duplicate
artifacts may merge in production and are discounted here. **Same-provider**
duplicates and artist-vs-track **ranking** problems are unaffected by the skip and
are real. The tool also only exercises the **search** path — discography/detail
enrichment (`get_artist_content.go`, `consensus.go`, `find_related.go`) is a
different code path and is NOT covered here (needs its own harness — see Gaps).

---

## THE dominant trend — artist-intent rank inversion

**The ranker lets multi-source track-*title* noise outrank the *artist* the query
is for.** Source-count (how many providers returned an entity) beats
artist-name-exactness. A single-source or common-word artist loses to a pile of
unrelated tracks that merely contain the query word in their title.

Verified instances (position = where the intended ARTIST landed in final top-20):

| Query | Intended | Landed | Beaten by |
|---|---|---|---|
| `glaive` | artist glaive | pos 9 | tracks titled "Glaive" (the weapon) |
| `Rosalía` | artist ROSALÍA | pos 6 | tracks titled "Rosalía" (a given name) |
| `Tool` | the band | absent top-5 | tracks titled "Tool" |
| `Yes` | the prog band | pos 11 | tracks titled "Yes" |
| `Air` | the duo | pos 6 | "Air"-titled tracks/albums |
| `Bones` | the rapper | pos 5 | Imagine Dragons / Radiohead "Bones" |
| `somber` / `sombre` | artist sombr | absent top-20 | literal "Somber"/"Sombre" tracks |
| `billy eilish` | Billie Eilish | pos 18 | "Billy" literal matches |

**Why it matters:** one rank-side change — *an exact artist-name match must resist
burial by tracks that merely contain the query word, regardless of src-count* —
fixes underground artists, common-word band names, **and** most phonetic/typo
cases at once. This is the highest-leverage fix found. It is NOT an alias problem
and NOT a query-rewrite problem.

---

## Secondary trend — same-provider duplicate-artist explosion

Deezer returns many **distinct artist records sharing the exact same name**, and
the pipeline never merges them. The diversity cap only *hides* the count by
truncating, it doesn't collapse them. Counts observed in STAGE-3 top-10:

- `che` → 5 distinct Deezer "Che"/"Ché" artist rows (pos 0–4)
- `Air` → 6 Deezer "Air" artist rows
- `Bones` → 5
- `Tool` → 4
- `Yes` → 2

These are **same-provider**, so the identity bridge would not fix them. They burn
head slots and compound the rank inversion above. (Caveat: some genuinely ARE
different artists with the same name — the fix is about not letting N unknown
same-name artists occupy the top, not blind-merging them.)

---

## What works (baseline is strong — do not touch)

- **Mainstream single-name artists:** Drake, Taylor Swift → pos 0.
- **Multi-token "artist + album/song":** `Kendrick Lamar HUMBLE`, `Radiohead OK
  Computer`, `Tyler the Creator IGOR`, `Frank Ocean Blonde` → **all pos 0**, via
  large multi-source merges. The strongest path in the whole battery.
- **Strong multi-provider artists / stylized handles:** Björk, Sigur Rós, Bad
  Bunny, NewJeans, yeat, brakence, `$NOT`, `Ke$ha` → pos 0.
- **Diacritics:** `Björk`/`Bjork`, `Sigur Rós`/`Sigur Ros` fold to the same
  canonical artist; folded form is sometimes *richer*. **Not a problem.**
- **High-consensus tracks:** `Water` (Tyla) → pos 0.

---

## Plan corrections (this audit overturns the program doc)

1. **Phonetics is RANK-side, not query-side.** The program doc claimed `somber`
   returns zero entities carrying a `sombr` token and "providers never return the
   artist, so it must be a query rewrite." **Did not reproduce.** Deezer's fuzzy
   search returned the artist sombr (artist-search pos 3) and his real tracks
   (`back to friends`, `undressed`, `we never dated`) for both `somber` and
   `sombre` — they were ranked out by literal matches. ⇒ Phonetics may fall out of
   the artist-intent rank fix for free; the deferral premise needs re-checking
   before any hot-path query-rewrite is built. **Action: re-verify, then likely
   un-defer as part of fix #1.**

2. **Alias resolution is barely needed.** `$NOT` and `Ke$ha` landed at pos 0 —
   symbols did not break recall. The only true recall failure was `che cxo`, and
   that's because the artist exists **only as UGC** (no provider has a canonical
   entity) — no alias table fixes that. ⇒ "Aliases first" (the earlier
   recommendation) is **not supported by the data**; demote it.

3. **Diacritics workstream — drop.** Folding already works.

---

## Genuine catalog-absence (separate class)

`che cxo` — intended underground artist returns **zero** artist entities anywhere;
only UGC track noise (`CHE - KISS PROD CXO` type-beats on SoundCloud/Last.fm). No
ranking or alias fix helps; the artist's canonical home isn't in the searched
providers. Smallest addressable class; possibly out of scope, or needs a
provider that hosts UGC artists as first-class entities.

---

## Data-driven priority (replaces aliases-vs-phonetics)

1. **Artist-intent rank fix** — exact artist-name match resists title-noise
   burial. Subsumes glaive / Rosalía / Tool / Yes / Air / Bones / sombr / typos.
   *Hot path → implement behind a default-OFF flag, eval-gated (like tail
   demotion).*
2. **Same-provider artist dedup / head-slot diversity** — stop N unknown same-name
   artists occupying the top.
3. **Re-test phonetics deferral** — likely solved by #1; verify before building
   query-rewrite machinery.
4. **Catalog-absent artists** (`che cxo`) — smallest, possibly out of scope.

---

## Gaps this audit did NOT cover (for a later session)
- **Discography / detail-screen correctness** (program workstream #3): chronological
  order + year completeness. Different code path; `discoverytrace` can't see it.
  Needs a discography trace harness.
- **Behavioral-telemetry trends**: now that events land, real user search →
  outcome trends should be mined from `discovery_events`. Not done here.

## Second-wave trends (30 more queries, 5 new dimensions)

**Producers / features / collabs.** Producers-as-artists resolve **clean** (Metro
Boomin, DJ Khaled, Jack Antonoff, `21 Savage Metro Boomin` collab — all pos 0).
Two failures: (a) **`feat`/`ft` trailing token** — `Calvin Harris feat` never
surfaces the artist; the dangling token makes Deezer return dozens of phantom
"Artist feat. X & Y" composite-artist rows (src=1 each) that flood the slate. (b)
`Drake ft Rihanna` — canonical multi-source tracks ("Too Good" lastfm src=7) rank
**below** single-source SoundCloud bootlegs/remixes (the tail-noise pattern).

**Ambiguous song titles.** Mostly **clean** — src-count acts as an effective
popularity proxy: `Hello`→Adele, `Stay`→Rihanna, `Closer`→Chainsmokers,
`Sunflower`→Post Malone all win pos 0. Two failures: `Without You` →
**wrong-kind-wins** (an *artist* literally named "Without You", src=13
SoundCloud-inflated, beats Mariah Carey's track); `Forever` → obscure "The Little
Dippers" (src=8, catalog-duplicated) outranks Drake — src-count rewards
duplicated obscurities because there's no true popularity signal.

**Numeric / punctuated band names.** **Clean** — `1975`, `twenty one pilots`,
`100 gecs`, `311`, `blink-182`, `AJR` all land pos 0–1. Normalization
(lowercasing, hyphen-strip) does NOT damage recall. Only residual src=1 MB
spoken-word/parody tail noise. **Not a problem.**

**Classical / jazz / CJK.** Jazz artist+album clean (`Miles Davis Kind of Blue`
pos 0). Two real weak spots: (a) **Classical composer-vs-common-word** —
`Beethoven` is composer-chaos (rap/pop tracks titled "Beethoven" beat the
composer, who's absent from top-5); `Tchaikovsky` ranks pos 0 but exposes the
same-provider exact-dup ×10 (Craig Austin). (b) **CJK normalization** — `坂本龍一`,
`宇多田ヒカル` are NOT recall failures (providers return rich results) but
`qnorm=""` because `textnorm.NormalizeForMatch` strips all CJK codepoints ⇒
relevance scoring is disabled ⇒ the canonical artist sinks to #11–#14 while a
noisy high-src collab/karaoke track wins #0. Also: iTunes reliably 403/429s on
CJK terms.

**Variant editions / recency / cataloged-underground.** `Taylor's Version` and
`remix` correctly avoid collapsing to the original, but single-source UGC/novelty
(lullaby covers, piano tributes, SoundCloud bootlegs) **outrank the legit
variant** — the tail-noise pattern at the *head*. Recency is fine (`GNX` 2024
present, pos 1). `MF DOOM` / `Madvillainy` are **clean** (pos 0) — confirming the
earlier `che cxo` failure was about *non-existent catalog*, not underground-ness.

---

## Consolidated taxonomy (grounded in the code)

Read `rank.go` + `rank_relevance.go` + `merge.go`. The mechanism behind almost
every failure: relevance is **asymmetric IDF-weighted coverage**; for a bare
single-token query an exact artist-name and an exact track-title **both score
~1.0 and tie**, then the `rankLess` ladder (popularity[inert] → multi-source →
RRF) breaks the tie toward the **multi-source track**. Classifying the findings:

- **A — Inherent ambiguity ceiling (documented, NOT a bug).** Bare single-token
  artist-vs-track: `Tool`, `Yes`, `glaive`, `Rosalía`, `Bones`, `Air`. The
  discovery `CLAUDE.md` accepts ~81% top-3 here; a popularity fix targeting this
  was **reverted** (regressed the eval). *Open question worth an eval A/B: does
  "full query == artist canonical name" deserve a relevance edge? That's a
  principled signal, not a kind-tier.*
- **B — Principled merge tradeoffs (NOT bugs, by design).** Deezer same-name
  artist explosion = the `ambiguousArtistNames` guard (refuses to merge when MB
  shows 2+ MBIDs). `Tchaikovsky ×10` = identifier authority (distinct recording
  MBIDs never merge). Changing either risks the 0%/0% merge gate.
- **C — Real bugs, RISKY to fix (core/normalization, must be eval-gated).**
  - **CJK `qnorm=""`** — `textnorm` strips CJK so CJK queries are unrankable.
    Clear root cause, high value, but `textnorm` is shared by merge+rank+eval.
  - **Rank-side burial of the fuzzy-correct entity** — `somber`/`sombre`→sombr,
    `billy eilish`, `Without You`. Same relevance-tie-then-src-count family as A.
- **D — Real bugs, SAFE-ish to fix.**
  - **`feat`/`ft` trailing dangling token** (query hygiene in `query_clean.go`) —
    narrow, low-risk, low-frequency. *(Implemented this session — see below.)*
  - **UGC-above-canonical** (`Drake ft Rihanna`, variant novelty) — this is
    exactly what the **already-built, default-OFF tail-noise demotion** targets.
    Not new code; needs the eval A/B gate to flip `TAIL_DEMOTION_ENABLED`.
- **E — Unfixable / out of scope.** `che cxo` (catalog-absent UGC-only artist);
  "no true popularity signal" (`Forever`) — known and reverted, don't redo naively.

---

## Deferred implementation plan (for a later chat, each with its eval gate)

1. **Tail-noise demotion turn-on.** Evidence keeps mounting (Drake ft Rihanna,
   variant novelty, src=1 MB tails). Re-run the demotion A/B on clean fixtures;
   flip `TAIL_DEMOTION_ENABLED` if top-3 holds. *Lowest effort — code exists.*
2. **CJK-aware normalization.** Make `textnorm.NormalizeForMatch` preserve CJK
   codepoints so `qnorm` is non-empty and relevance can rank. Gate: exact corpus
   must stay 100% top-3; add CJK spot-checks. *Clear root cause, high value.*
3. **Artist-intent exact-name edge.** Eval-A/B whether `query == artist canonical
   name` earns a relevance bump that survives the exact corpus. The principled
   reframe of finding A/the "inversion." The popularity revert warns this is
   delicate — same-sample A/B mandatory, never ship blind.
4. **`feat`/`ft` hygiene.** Done this session.

## Implemented this session

- **`feat`/`ft`/`featuring` trailing-token query hygiene** (`query_clean.go` +
  `query_clean_test.go`, new `TestCleanQueryTrailingFeat`). A dangling trailing
  feature marker is stripped before fan-out; a mid-query `feat <artist>` is left
  intact. Wired into production via `search.go:240` (`CleanQuery` → `fanOut`).
  Verified: `go test ./internal/discovery/... -count=1` → 665 pass; `go vet` clean;
  `go build ./...` clean. Cannot affect the library eval corpus (no library query
  ends in a bare "feat"), so the 100% top-3 gate is unchanged. **Not committed** —
  left in the working tree on `feat/discovery-tail-noise-demotion` for review.

### Eval baseline captured (2026-06-27)
`go run ./cmd/discoveryeval -mode eval -limit 25` → **Top-1 100%, Top-3 100%**
(base 0.985 / 0.995, ok). Confirms every trend above lives *outside* the exact
`"artist title"` corpus — the gate is healthy and the failures are in
bare-token / CJK / feat-residue territory the corpus doesn't exercise.

- **CJK-aware normalization** (`textnorm/normalize.go` + tests). Replaced the
  ASCII-only `[^\w\s]` regex (which deleted every CJK/non-Latin letter → `qnorm=""`
  → relevance disabled for an entire script class) with `stripSymbols`, a
  Unicode-aware filter that keeps letters of any script while still dropping symbols
  and hyphens. **Proven ASCII-byte-identical** (`TestStripSymbolsASCIIByteIdentical`
  brute-forces all 128 ASCII bytes vs the old regex) so it can't perturb the Latin
  corpus; matching stays symmetric. Also recompose to NFC in `stripDiacritics` so
  Hangul returns canonical syllables. Verified: textnorm 44 pass, discovery 665
  pass, vet/build clean, **exact eval still 100% top-1/top-3 (no regression)**, and
  `discoverytrace "坂本龍一"` now shows `qnorm="坂本龍一"` (was empty) with relevance
  ranking active. Residual: the single-token artist-vs-track tension still applies
  to CJK (a track titled with the artist name can top the artist entity) — that's
  finding A, not this fix. **Not committed** — working tree.

### Not implemented (deferred to a later chat — by design, see plan above)
The high-value fixes (tail-demotion turn-on, CJK normalization, artist-intent
exact-name edge) all touch the eval-gated ranking/normalization core and a
documented revert history says "don't redo naively." They need same-sample A/B
runs that warrant being done with you in the loop — not shipped blind in the
background. Each has its eval gate spelled out in "Deferred implementation plan."
