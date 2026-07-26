# Detail screens rework

> Spec for `detail-screens-rework` — version 1, drafted 2026-07-26.
> Authors: solo + Claude.
> Status: Ready-for-plan.
> Mockups: [`docs/notes/screen-atlas.html`](../../notes/screen-atlas.html) (current state, all screens) — chosen direction is **L2/W2**, reproduced in §Chosen design.

## Problem

`DetailScreen` is one route serving three different objects, and the three bodies have drifted apart. Track detail stacks Play and Save vertically and centred; album detail puts them side by side in a row; artist detail uses a single full-width button. The album's own metadata (`2013 · 8 tracks · 48 min`) renders *below* the tracklist, hundreds of pixels from the title it describes. There is no title bar — a bare back chevron sits above a 200px centred hero, so scrolling costs you all context.

The centred hero also wastes the screen. On a track, the primary action sits roughly 420px down; the top third of the screen is artwork and the right half of every metadata row is empty. The user's summary of the current design: *"there's so much open space"*.

## User value

- Play is reachable without scrolling on all three kinds — it moves from ~420px to ~340px, in a labelled target instead of a 44px circle.
- The screen tells you what the thing *is* at a glance: length, release year, whether you're hearing a preview or the real file, how many releases an artist has, how many of them you own. Some of this is currently buried below the fold; some is not shown at all.
- Album and artist and track feel like the same app. Today they demonstrably don't.
- Artwork gets a full-bleed 318px banner instead of a 200px square with margins on both sides.

## Scope tier / MVP cut

- **Minimal (ship this):** one shared scaffold (`DetailScaffold`) that all three bodies render into — banner, action row, fact row, sections. Rework `TrackDetailBody`, `AlbumDetailBody`, `ArtistDetailBody` to fill its slots. Presentational only: no new API calls, no new queries, no new external data.
- **Deferred to post-launch:** scroll-linked banner parallax/collapse animation; artwork colour extraction for a per-object accent; light-theme design pass (still ADR-0008 debt); adding "Add to Playlist"/"Add to Queue" to the detail ⋯ menu.
- **Justified exceptions:** none.

All facts on the new fact row come from data the screen already holds — `trackExtras`, `albumExtras`, `useDetailEnrichments`, `useOwnedTrack`, `useAlbumDetailState`, `useArtistDetailState`. **If a value is unavailable, its cell is omitted rather than rendered empty.**

## Chosen design

One scaffold, four slots, top to bottom:

1. **Floating app bar** — translucent circular back button left, ⋯ right, over the banner. Gains a solid `canvas` background and the object title once the banner scrolls past.
2. **Banner** — the artwork, cover-cropped, full-bleed, `318px`, running to `y=0` behind the status bar. Gradient scrim to `canvas` at the base. Carries **only identity**: title at `38px/41` (`letterSpacing: -1.2`, clamped to 2 lines) and a secondary line — artist for a track/album, genres for an artist.
3. **Action row** — full width. Primary `Play` is a **labelled pill that flex-grows**; secondary controls are 44px icon buttons trailing it, so the row ends exactly at the right margin.
4. **Fact row** — up to three labelled cells, `justifyContent: 'space-between'`, under a hairline. Uppercase 10px label over a 15px semibold value.
5. **Sections** — one `Section` component (uppercase label left, optional accent action right) whose body is rows, a horizontal rail, or a grid.

Per-kind slot contents:

| | Secondary line | Actions | Facts |
|---|---|---|---|
| Track | artist | Play preview · Save | Length · Released · Source |
| Album | artist | Play · Shuffle · Save N | Tracks · Runtime · Released |
| Artist | genres | Play · Shuffle | Releases · In library · Listeners |

Every action in that table exists today. The mockup showed a download icon on artist detail; that is **not** in scope — there is no download-an-artist action in the app and inventing one is out of the presentational remit.

The `Source` cell reads `Preview` when the only playable source is a 30-second preview and `Library` when the owned file is playable. If neither can be determined, the cell is omitted per AC#6.

Sections keep their current order and content. **`About` stays exactly where it is** on artist detail — the only change is that it is a normal section instead of being collapsed inside a `Disclosure`.

### Content changes

- **Album name leaves the track's metadata line.** It is already a `Details` row with artwork and a chevron, where it is actually tappable — having it in the meta line too was duplication, and it was what made the line read as a run-on.
- **`Wrong album?` moves into the ⋯ menu.** It is a report action, not content; it does not deserve a tap target in the body.
- **Genre/similar-artist pills are replaced.** Genres move to the banner's secondary line; similar artists become rows with avatars and chevrons. Today a genre and an artist name render as identical grey pills.
- **Album metadata moves up** from below the tracklist into the fact row.
- **`Source: Preview` replaces the amber preview banner.** Same information, no special-case widget.
- **Removed with no replacement:** the album save/download progress bar and any `n of m saved` count. Explicitly rejected by the user — *"it's not a completionist sort of thing"*. Per-track save state stays on each row.
- **Removed:** the centred `n still downloading` caption on album detail.

## Acceptance criteria

1. **AC#1** — Given any detail route, when it renders, then the app bar, banner, action row, fact row and sections appear in that order, and all three kinds resolve their layout from the same scaffold component.
2. **AC#2** — Given a track whose only playable source is a preview, when the screen renders, then the fact row shows a `Source` cell reading `Preview`, and no separate preview warning banner is rendered anywhere. Given a track whose owned file is playable, that cell reads `Library`.
3. **AC#3** — Given an album, when the screen renders, then track count, runtime and release year appear in the fact row above the tracklist, and no saved/downloading progress bar or `n of m` completion count is rendered.
4. **AC#4** — Given a track with a known album, when the screen renders, then the album name appears exactly once — as a `Details` row — and not in any metadata line.
5. **AC#5** — Given any detail route, when the user scrolls past the banner, then the app bar has an opaque background and shows the object's title.
6. **AC#6** — Given a fact whose underlying value is absent, when the screen renders, then that cell is omitted and the remaining cells still span the full content width.
7. **AC#7** — Given a track with a known album, when the user opens the ⋯ menu, then a `Wrong album?` action is present and reports on tap; and no such control is rendered in the screen body.
8. **AC#8** — Given an artist with Last.fm enrichment, when the screen renders, then the bio appears as a normal section and similar artists render as rows, not as pills.
9. **AC#9** — Given any detail route with a title long enough to wrap, when it renders, then the title clamps to 2 lines and the action row does not move.
10. **AC#10** — Existing behaviour is preserved: every save, play, retry, lateral-navigation and enrichment interaction covered by the current `features/detail/__tests__` suite still passes.

## Out of scope

- The other detail-adjacent screens: `FeaturingScreen`, `PlaylistDetailScreen`.
- Any change to what the detail routes fetch, or to `getDetailHandoff` / ADR-0017 module-state handoff.
- Splitting the one route into three (considered — see below).
- Scroll-linked animation beyond the app-bar background crossfade.
- Light theme. These mockups are dark-only; ADR-0008 debt is untouched.
- New actions in the ⋯ menu beyond the displaced `Wrong album?`.

## Design considerations

Lexicon lookup (`~/.claude/lexicon/MANIFEST-ts.md`):

- **[lexicon: `ts/behavioral-patterns/template-method-pattern`]** — this is the shape: a skeleton that fixes the sequence of steps and defers individual steps to variants. Its manifest guidance applies directly — *"Avoid Overriding the Template Method"* and *"Keep the Template Method Simple"*: the scaffold owns order and spacing, bodies own only slot content. Its **inheritance mechanism loses** here: CLAUDE.md bans class components, and subclass coupling is exactly the drift we are fixing. Realised instead as composition — `DetailScaffold` takes `banner`, `actions`, `facts` and `children` as props.
- **Rejected: [lexicon: `ts/structural-patterns/composite-pattern`]** — a recursive section tree. *Cost:* the sections here are a flat list of three to six known kinds; recursion buys nothing and makes the render order implicit.
- **Rejected: [lexicon: `ts/behavioral-patterns/strategy-pattern`]** as a per-kind layout strategy object. It would let the three kinds diverge again behind an interface, which is the failure mode, not the fix.
- **Considered and rejected: three separate screens.** Mocked up as direction B. Individual screens came out better — the full-bleed artist page was the strongest single frame — but nothing structurally prevents a re-drift, and re-drift is the bug. Chosen: shared scaffold, with the banner able to vary by kind inside it.

High-level approach:

- Pure **read path** in the `detail` mobile feature slice. No backend change, no Go touched.
- Requires **no** new aggregate, value object or port. No new external dependency, so **no ADR needed**. ADR-0008/0009 (design system, cobalt-charcoal) still govern; all colour, spacing and radius values come from `@shared/ui/theme` tokens.
- The `38px/-1.2` display size and the `10px` uppercase fact label are **new type steps** not in `tokens.ts`. They must be added to the `typography` scale as named variants, not inlined — inline styles that should be theme tokens are a "never ship" item in `apps/mobile/CLAUDE.md`.
- The scaffold is a new shared-shape component but has exactly **three** consumers in one slice, so it belongs in `features/detail/ui/`, **not** `shared/ui/` (extraction to `shared/` requires 2+ consumers *across* features).

## Dependencies

- **Bounded contexts**: none new. Reads `catalog` + `discovery` shapes already in hand.
- **Other features**: none blocking. Touches `features/detail/` only; `@shared/ui/primitives` gains no new component.
- **External services**: none.
- **Library/framework additions**: none. `expo-linear-gradient` (banner scrim) and `expo-image` (cover crop) are already dependencies.
- **Repo hygiene, same commit**: `features/detail/ui/CLAUDE.md` file map (files added/renamed), `okf/mobile/detail-feature.md` (20.6 KB — describes the current three-body layout in detail and will be substantially wrong), and `tokens.ts` type-scale additions. The OKF pre-commit hook will block otherwise.

## Risks / open questions

- **Risk: banner legibility on uncontrolled artwork.** Only the title and secondary line sit on the image; facts and controls are on canvas. This is why L2 was chosen over placing facts or Play on the scrim. Mitigation: scrim reaches 90% opacity at the base. Verify against a deliberately pale, high-detail cover.
- **Risk: square art cover-cropped to a wide banner loses the edges.** Fine for most covers, bad for artwork with detail at the margins. Mitigation to evaluate during implementation: blurred fill behind a contained image when the crop is judged bad. Not in the minimal tier.
- **Risk: `space-between` on three cells means the middle cell is not on a fixed grid** across the three kinds — visible when flipping between them quickly. Accepted; the fixed-column alternative (W3) added three vertical rules to a screen that already has section and row separators.
- **Risk: regression surface.** `TrackDetailBody`, `AlbumDetailBody` and `ArtistDetailBody` are ~1,000 lines carrying real save/play/nav state. AC#10 exists to hold this; the existing `__tests__` suite is the gate. Behaviour must not change — only presentation.
- **Assumption (flag, not blocking):** on album and artist the ⋯ menu has no items once `Wrong album?` is track-only, so it is **hidden when empty** rather than rendered as a dead affordance. If the intent is for it to always be present, it needs items, which is deferred scope.
- **Open question:** does the fact row belong above or below the first section on artist detail, where `Releases · In library · Listeners` overlaps conceptually with `About`? Resolved for now as: above, consistent with the other two kinds. Revisit if it reads as redundant in the running app.

## Telemetry

Presentational change; no new domain events. Worth confirming the value claim rather than adding instrumentation:

- **Existing events to watch**, not new ones: the detail-screen save and play interactions already emitted by `useSaveTrack` / `usePlayback`. If Play truly moved into reach, play-from-detail rate should not *fall*.
- **Metrics**: none new.
- **Alerts**: none.

## Related

- `[lexicon: ts/behavioral-patterns/template-method-pattern]`, `[lexicon: ts/structural-patterns/composite-pattern]`, `[lexicon: ts/behavioral-patterns/strategy-pattern]` — applied/rejected above
- Related ADRs: `docs/adr/0008-mobile-design-system.md`, `docs/adr/0009-visual-refresh-cobalt-charcoal.md`, `docs/adr/0017-detail-handoff-module-state.md`
- Predecessor spec: `docs/specs/view-result-detail/spec.md`
- Concept doc to update: `okf/mobile/detail-feature.md`
- Mockup archive: `docs/notes/screen-atlas.html`
