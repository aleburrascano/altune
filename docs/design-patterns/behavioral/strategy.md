# Strategy — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/strategy

**Intent.** Define a family of interchangeable algorithms, encapsulate each, and swap them behind a common interface.

## Problem
A class accretes multiple variants of one operation (per provider, per kind, per ranking scheme), bloating it and forcing risky edits whenever a variant changes. A `switch` selecting the variant spreads the algorithm's guts across the class.

## Solution
Extract each algorithm behind a small common interface (or a function value); the context holds one and delegates. Algorithms are added, swapped, or tested in isolation without touching the context.

## In altune
**Go:** The **dominant pattern in this codebase** — interface satisfaction and function values, no inheritance. The discovery ports are 1–3-method strategies the service composes: `MetadataEnricher`, `LastFmEnricher`, `DiscogsEnricher`, `DeezerEnricher`, `LyricsProvider`, `SearchProvider`, `AlbumContentProvider`, `ArtistContentProvider`, `RelatedTracksProvider` (all in `services/go-api/internal/discovery/ports/`). Each provider adapter is one concrete strategy satisfying the port structurally. A passed-in `func(...)` is the lightweight form — e.g. a `fetch(ctx, parse func(...))` shape parameterizes the varying step. The whole `Rank` rebuild was a Substitute-Algorithm swap behind a stable strategy seam (`service/rank.go`).
**RN/TS:** A **strategy lookup table** keyed off a discriminated-union tag — `Record<Kind, () => …>` or `Record<Kind, Component>` — replaces a scattered `switch (kind)`. A `variant` prop selecting render behavior is the component-level form.
<Verified: ports listed in `services/go-api/internal/discovery/ports/ports.go` (MetadataEnricher:13, DiscogsEnricher:58, LastFmEnricher:82, LyricsProvider:115) and `ports_search.go` (SearchProvider:10, AlbumContentProvider:27, ArtistContentProvider:31, RelatedTracksProvider:40).>

## When to reach for it
- 2+ interchangeable implementations of one operation (multiple providers, ranking schemes).
- Isolating a swappable algorithm for testability (inject a fake strategy).

## When to skip it
One algorithm, one caller, no second implementation in sight — "discover interfaces, don't design them." A single local `switch` beats a strategy table until the variants stabilize (Rule of Three). Extraction to `shared/` still needs 2+ real consumers.

## When Strategy vs State / Template Method
[[strategy]] = independent, client-chosen algorithms (composition). [[state]] = variants that transition between themselves. [[template-method]] = fixed skeleton with pluggable steps (Go: pass the steps as function values).

## Related
- Patterns: [[state]], [[template-method]], [[command]] (same struct shape, different intent)
- Refactoring moves: `../../refactoring/simplifying-conditional-expressions.md` (Replace Conditional with Polymorphism), `../../refactoring/dealing-with-generalization.md` (Extract Interface; Form Template Method), `../../refactoring/composing-methods.md` (Substitute Algorithm)
- Project rules: `../../../.claude/rules/backend/go-design-patterns.md`, `../../../.claude/rules/backend/go-structs-interfaces.md`, `../../../.claude/rules/frontend/rn-state-management.md`
