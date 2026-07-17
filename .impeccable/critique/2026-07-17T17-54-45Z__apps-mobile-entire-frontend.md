---
target: entire frontend
total_score: 27
p0_count: 1
p1_count: 3
timestamp: 2026-07-17T17-54-45Z
slug: apps-mobile-entire-frontend
---
Method: dual-agent (A: aea959a5 design-review · B: a4adcb32 deterministic-evidence)
Target: Altune mobile — entire frontend. Register: product / native mobile (Expo RN). Detector: HTML/CSS-only, not applicable to RN source (attempted, exit 0, empty).

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | Real skeletons, `pending` search hairline, download-phase labels, save lifecycle. Strong. |
| 2 | Match System / Real World | 4 | "Now Playing", "Up Next", cobalt play, art-row grammar — earned familiarity. |
| 3 | User Control and Freedom | 2 | "Remove from Library" deletes with no confirm and no undo — the only unguarded destructive action. |
| 4 | Consistency and Standards | 2 | Three text-input focus vocabularies; two loading idioms; QueueSheet color/weight escapes. |
| 5 | Error Prevention | 2 | Validation gates submit (good); no removal confirm; no password-visibility toggle. |
| 6 | Recognition Rather Than Recall | 3 | Recent-search chips, visible filter/sort chips, persistent controls. |
| 7 | Flexibility and Efficiency | 3 | Inline preview-play, swipe-remove, shuffle/repeat, context menus, haptics. |
| 8 | Aesthetic and Minimalist Design | 4 | Genuinely clean; detail screen deliberately decluttered. |
| 9 | Error Recovery | 3 | Retry buttons, plain copy, typed auth errors, failed-row retry with reason. |
| 10 | Help and Documentation | 1 | No onboarding/tooltips/first-run (acceptable for genre, still scored). |
| **Total** | | **27/40** | **Acceptable→Good — points lost on consistency + control, not craft.** |

## Anti-Patterns Verdict

**Not AI slop.** This reads as disciplined, token-driven work by someone who studied Spotify/Apple Music: art-forward rows, a single-scroll detail screen, a cobalt-glow full player, inline preview-play, swipe-to-remove queue, a four-state save→saving→saved→retry lifecycle. Token discipline is real and ~95% followed. The tells are concentrated, not pervasive.

**Deterministic scan:** the bundled HTML/CSS detector is not applicable to RN (exit 0, empty). Source-evidence scan found: only 2 true hardcoded-color violations (`QueueSheet.tsx:259` `#e55`, `:264` `#fff`); `useReduceMotion` defined but consumed in exactly ONE place (`Skeleton.tsx`) — press-scale springs and the queue reanimated motion ignore the OS reduce-motion flag; `textTertiary` (#74747E) fails WCAG AA 4.5:1 for body text on every shipped dark surface (~3.4–4.1:1), used as SearchBar placeholder and captions; no primitive has an error state; IconButton/Chip/PlayIconButton have no loading state. Accessibility baseline is strong (81 `accessibilityLabel`, IconButton makes label a required prop, zero `TouchableOpacity`, hitSlop on 16 small controls, touch targets ≥44).

## Overall Impression

A confident, coherent app that is closer to shipping-grade than most indie music clients — the token system, status/loading maturity, and designed-in accessibility are genuinely above the genre baseline. What holds it back is *consistency drift at the edges*: one destructive action skips the guard every other one uses, one screen (QueueSheet) breaks the design system it's surrounded by, three text inputs each invent their own focus vocabulary, and two loading idioms coexist. The single biggest opportunity: pick one pattern for each of these four things and enforce it. That alone moves consistency from 2→4.

## What's Working

1. **Token discipline is real and near-total.** Semantic color via `useTheme()`, one 4pt scale, a typed ramp with per-weight `fontFamily` to dodge faux-bolding — actually followed in ~95% of files. This is why the app feels coherent.
2. **Status/loading maturity beyond the genre.** Skeletons matched to final layout (`DiscoverBody.tsx:74`, `LibraryScreen.tsx:88`, `PlaylistDetailScreen.tsx:137`), a `pending` progress hairline, live download-phase labels, a four-state save lifecycle. Few indie apps get this far.
3. **Accessibility designed-in, not bolted-on.** `accessibilityRole/Label/State` pervasive; the scrubber is a proper `accessibilityRole="adjustable"` with `accessibilityValue`; skeletons hidden from AT; `IconButton` *requires* a label at the type level. Strong foundation.

## Priority Issues

**[P0] Destructive library removal has no confirmation or undo.**
- Why it matters: `LibraryScreen.tsx:74` fires `deleteMutation.mutate(track.id)` directly from the context menu — no `Alert`, no undo. It's the app's most damaging irreversible action and the *only* destructive action without a guard (Delete Playlist and Clear Queue both confirm). Users discover the inconsistency by losing data. Peak-end damage.
- Fix: either match the existing `Alert.alert` confirm pattern, or (better) fire immediately behind an undo Snackbar and then drop confirms everywhere for a faster, consistent app.

**[P1] `textTertiary` fails WCAG AA contrast for body text on every shipped surface.**
- Why it matters: #74747E on canvas ≈4.1:1, on surfaces ≈3.4:1 — below the 4.5:1 body floor. It's the SearchBar `placeholderTextColor` (`SearchBar.tsx:60`) and caption/section-header text (`DiscoverBody.tsx:121`). Placeholder + captions are exactly the low-vision exposure. Dark theme is what ships, so this is live.
- Fix: lighten `textTertiary` toward ~#8A8A94+ until placeholder and body-size caption pairs clear 4.5:1; keep the darker tone only for ≥18px/large text if wanted.

**[P1] Auth text entry: invisible focus, no autofill, no password-visibility toggle.**
- Why it matters: `SearchBar` shows an accent focus border; `AuthForm` inputs (`AuthForm.tsx:171`) have *no* focus indicator, and lack `textContentType`/`autoComplete` for email/password/newPassword — breaking 1Password / iCloud Keychain / Google autofill at the highest-abandonment moment. No visibility toggle raises typo-driven failed logins. Three inputs (SearchBar, AuthForm, CreatePlaylistModal) each invent their own field vocabulary.
- Fix: promote one `TextField` primitive to `shared/ui` with a focus-accent border, wire `textContentType`/`autoComplete` per field, add an eye toggle, and route auth + modal inputs through it.

**[P1] Queue: false reorder affordance + gesture-only, AT-unreachable removal.**
- Why it matters: `GripVertical` handles (`QueueSheet.tsx:112`) promise drag-to-reorder that doesn't exist — a dead affordance a power user notices immediately. The only remove path is a right-swipe (`:104`) with no button or menu fallback, so screen-reader and motor-impaired users cannot remove a track at all.
- Fix: either build drag-reorder or delete the grip glyph; and add an accessible remove affordance (trailing "×" IconButton or a row context-menu item) so removal isn't gesture-exclusive.

**[P2] QueueSheet breaks the design system it sits inside; loading idiom split app-wide.**
- Why it matters: raw `fontWeight: '700'/'600'` (`:238,:248`) contradicts the per-weight-fontFamily rule (faux-bold/no-op risk), hardcoded `#e55`/`#fff` (`:259,:264`) will break the moment light theme ships, and `fontSize: 10` (`:237`) sits below the caption floor. Separately, Detail and Player use a bare `ActivityIndicator` while Discover/Library/Playlist use `Skeleton` — two loading languages.
- Fix: route QueueSheet labels through `Text` variants and `theme.color.danger`/`onAccent`; standardize on skeletons for content loading (or a documented single spinner rule) across Detail and Player.

## Persona Red Flags

**Casey (distracted, one-handed):** Detail back is a tiny top-left `‹ Back` text link (`DetailScreen.tsx:114`) — hardest right-thumb reach and a smaller target than the `ChevronLeft` IconButton used on PlaylistDetail (inconsistent *and* unreachable). MiniPlayer Skip-Next only renders when `hasNext && !isPreview` (`MiniPlayer.tsx:105`), so the control flickers between tracks and defeats muscle memory.

**Sam (accessibility-dependent):** Queue removal is swipe-only with no AT alternative (`QueueSheet.tsx:104`) — a hard block. Auth inputs rely on `placeholder` as their only label (`AuthForm.tsx:87`) — no `accessibilityLabel`; placeholders vanish on input. `textTertiary` captions fail AA (above). Press-scale and queue motion ignore the reduce-motion flag.

**Alex (power user):** The grip handle that doesn't reorder is the standout dead affordance; queue has no drag-reorder at all — the one queue table-stake missing. Worth confirming whether "play next" vs "add to queue" is distinguished in `trackMenu.ts`.

## Minor Observations

- Same entity, two nouns: a track is "Song" on Discover but "Tracks" in Library chips — deliberate per docs (consumer-facing "Song" mirrors Spotify) but a conscious cross-screen ruling worth recording.
- Three modal paradigms coexist (`ActionSheet` slide-up, `ContextMenu` floating-anchored, `CreatePlaylistModal` centered card, plus full-route `QueueSheet`) — no single bottom-sheet grammar.
- Backdrop/dismiss overlays (`CreatePlaylistModal.tsx:43`, `ContextMenu.tsx:62`, `AddToPlaylistSheet.tsx:109`, `DiscoverScreen.tsx:31`) are unlabeled tappable regions — add `accessibilityViewIsModal` + a labeled close.
- `IconButton` disabled state is behavioral-only (no built-in visual dim; caller must pass a dimmed color) — easy to forget, easy to ship a live-looking dead button.
- Settings is spartan; the "Resolve featured artists" backfill (a maintenance action) sits at the same visual weight as Sign Out.
- `Banner` primitive is shipped but only used in auth/oauth error — near-dead surface area.
- Light theme is explicitly un-tuned (ADR-0008) — fine for dark-only v1, but QueueSheet's hardcoded hex is a landmine for when light ships.

## Questions to Consider

- If library removal is the deliberate inverse of saving, is the real answer an **undo Snackbar** that lets you drop confirms *everywhere* and speed the whole app up?
- Should the **Library home** get the detail screen's declutter treatment, collapsing its always-visible search + 4 chips + sort chrome into a hierarchy that expands on intent?
- You ship a reusable token system but three hand-rolled text inputs — what's kept `TextField` from being promoted, given the 2-consumer bar is already met?
- Build queue reorder, or admit the queue is "jump + remove" and delete the grip glyph?
- Could **preview-playing a whole list** make the deferred album/artist "Play" buttons honest instead of absent?
