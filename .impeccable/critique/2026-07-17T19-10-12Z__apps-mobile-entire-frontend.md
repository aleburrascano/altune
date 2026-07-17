---
target: entire frontend
total_score: 27
p0_count: 1
p1_count: 3
timestamp: 2026-07-17T19-10-12Z
slug: apps-mobile-entire-frontend
---
Method: dual-agent (A: a76f41e7 design-review · B: a9ba2226 deterministic-evidence)
Target: Altune mobile — entire frontend (post-remediation, commit f028229). Register: product / native mobile. Detector: HTML/CSS-only, not applicable to RN (attempted, exit 0, empty).

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | Skeletons, pending bar, download-phase labels. Missing a buffering state between tap and audio. |
| 2 | Match System / Real World | 3 | Good music vocabulary; undercut by literal `♫`/`+`/`‹` text glyphs standing in for icons. |
| 3 | User Control and Freedom | 2 | Destructive confirms present, but no undo anywhere and the queue can't be reordered. |
| 4 | Consistency and Standards | 2 | Still the weak axis: 3 back affordances, 2 remaining raw TextInputs, 4 play-button sizes. |
| 5 | Error Prevention | 3 | Validation gates submit, disabled buttons, destructive confirms (removal now guarded). |
| 6 | Recognition Rather Than Recall | 3 | Recent-search chips, visible sort, labeled tabs, persisted search. |
| 7 | Flexibility and Efficiency | 3 | Inline preview-play, swipe-remove, shuffle; no queue reorder, no long-press menus. |
| 8 | Aesthetic and Minimalist Design | 3 | Deliberate declutter, flat surfaces, one-scroll detail. |
| 9 | Error Recovery | 3 | Retry buttons + human error copy across discover/library/playback. |
| 10 | Help and Documentation | 2 | Empty states teach; no onboarding (acceptable for register). |
| **Total** | | **27/40** | **Same band — the fixed axes held; deeper re-read surfaced equally-weighted pre-existing issues.** |

## Anti-Patterns Verdict

**Not AI slop — assembly drift.** The token layer is ~85% enforced and genuinely disciplined (one `useTheme()` source, WCAG-tuned palette, display/body split held). What drags it is composition-level drift: the same action built differently across screens.

**Regression check — the six shipped fixes hold (verified by the deterministic pass):**
- `textTertiary` #8C8C96 now measures **5.62 / 5.10 / 4.70:1** on canvas/surface1/surface2 — passes AA everywhere (was 3.38–4.05).
- QueueSheet has **no hardcoded hex** anymore; swipe action uses `theme.color.danger`/`onAccent`; labels use `fontFamily`, not `fontWeight`; the side-stripe is gone (surface panel).
- `useReduceMotion` is consumed by **2 sites** now (`pressScale` + `Skeleton`); press feedback flattens under the OS flag.
- Library removal is guarded by an `Alert.alert` confirm; queue rows now have an accessible remove button.
- Modal dismiss backdrops (`ActionSheet`, `AddToPlaylistSheet`) carry a "Close" label; `TextField` primitive exists with focus border + secure show/hide + a11y-label fallback.

**Why the number didn't move:** three of the six items were the correct *specific* fixes, and two were deliberate deferrals (queue reorder, loading-idiom unification) — but the re-critique read deeper and surfaced issues of the same severity that were always present: a dead success-confirmation, a systematic accent-as-text contrast failure, and two more raw text inputs the first pass never reached. The score is holistic, not a checklist of the prior findings.

## Overall Impression

Still a confident, above-median app whose ceiling is set by consistency, not by any broken screen. The single biggest structural insight from this pass: the token system is rigorous but there's **no semantic-component tier** between primitives and screens — so "back", "play", and "text field" each got re-invented per screen. That missing tier (a `BackButton`, a `PlayControl`, universal `TextField` routing) is the root cause of the consistency-axis stall.

## What's Working

1. **Accessibility as a first-class constraint** — `minInteractiveHeight: 48` in tokens, IconButton hard-floors 44×44 with a type-required label, Skeleton hidden from AT, palette carries per-surface contrast math. 95 `accessibilityLabel`, 0 `TouchableOpacity`.
2. **Loading is skeletons, not spinners — for the screens that got the treatment** (discover, library, playlist, featuring), shape-matched and reduce-motion-aware.
3. **Empty states teach** — Discover CTA on empty library, "use the menu to add" on empty playlists, `LibraryNoResults` so a filtered library never reads as missing.

## Priority Issues

**[P0] The "Added ✓" success state is dead code.**
- Why it matters: in `AddToPlaylistSheet.tsx:44-50`, `onSettled` clears `addedTo` and closes the sheet the same tick `onSuccess` set it, so the confirmation checkmark never renders. The most-repeated library action completes with zero positive feedback — indistinguishable from a no-op. This is a real bug the first critique didn't catch.
- Fix: hold the checkmark ~700ms before closing (delay `onClose` in `onSuccess`, drop the eager `onSettled` close), or fire a global toast that survives the dismissal.

**[P1] `accent` (#2D5BFF) as text fails WCAG AA across ~22 sites.**
- Why it matters: cobalt-on-dark measures **3.0–3.6:1** — below the 4.5:1 body floor — yet it's used as normal/caption-size link and label text ("NOW PLAYING", "Clear", "See all", "Retry", auth links). It only clears the 3:1 large/UI floor. This is a bigger contrast surface than the tertiary text just fixed.
- Fix: introduce a lighter link tone for text-sized accent (the palette already has an unused `cobaltSoft` #5B82FF ≈ 4.7:1), or reserve `accent` for fills/large text and route small accent text through the lighter tone.

**[P1] Input + back-button vocabulary still isn't unified.**
- Why it matters: two more raw `TextInput`s remain — `ForgotPasswordScreen.tsx:50` and `SetNewPasswordScreen.tsx:44,53` (no focus state, no show/hide) — on the highest-anxiety account screens, bypassing the `TextField` primitive. Back is a text `‹ Back` on `DetailScreen` but a `ChevronLeft` IconButton on `PlaylistDetailScreen` — two screens in one stack disagreeing.
- Fix: route the two remaining auth inputs through `TextField`; extract one `BackButton` used wherever a stack pops. A lint rule banning bare `TextInput` outside the primitives would prevent recurrence.

**[P1] Queue has no reorder (deferred from the last pass).**
- Why it matters: drag-to-reorder is table-stakes in every reference music app; its absence reads as unfinished to a power user. The gesture-handler/reanimated infra is already present.
- Fix: add drag handles wired to the existing `reorderUpcoming` facade method.

**[P2] Loading-idiom split + two primitive gaps (partly deferred).**
- Why it matters: the detail bodies (album/artist/track) still use a bare centered `ActivityIndicator` while every other data screen uses `Skeleton` — the one loading-consistency defect left. Separately: `TextField` has no error state (callers hand-roll a danger `Text`), and `IconButton` disabled doesn't self-tint (relies on every caller to dim).
- Fix: skeletons for the three detail bodies; add an `error` prop to `TextField`; auto-dim `IconButton` when disabled.

## Persona Red Flags

**Casey (one-handed):** `AddToPlaylistSheet` gives no felt confirmation (P0) — taps a playlist, sheet closes, no idea if it worked. Detail back is a small text guillemet in the thumb-hostile top-left. Queue swipe-remove fires with no undo.

**Sam (accessibility):** Scrubber sets `accessibilityRole="adjustable"` + value but has no `accessibilityActions`/`onAccessibilityAction`, so AT can announce position but can't move it (`Scrubber.tsx:141`). `ForgotPasswordScreen`/`SetNewPasswordScreen` raw inputs have no focus indication. Accent-as-text links fail AA.

**Alex (power user):** No queue reorder, no long-press row shortcuts, no undo. Everything routes through the `⋮` menu.

## Minor Observations

- `PlaylistHero.tsx:123` hardcodes a `boxShadow` rgba (no shadow token exists); `AddToPlaylistSheet.tsx:168` backdrop uses literal `rgba(0,0,0,0.5)` while `theme.color.scrim` exists and is used by the peer `ActionSheet`.
- `AddToPlaylistSheet.tsx:126` "Create New Playlist" Pressable has no `accessibilityRole`/`accessibilityLabel`.
- `DetailScreen.tsx:222` `paddingBottom: 140` is an off-scale magic number (dock clearance).
- MiniPlayer progress animation doesn't gate on reduce-motion the way Skeleton does.
- The `shared-ui` CLAUDE.md note calling `Banner` "unused" is stale — auth error states use it.
- Two components named `PlayButton` (FullPlayer-local + `detail/ui/PlayButton`).

## Questions to Consider

- If the token system is this disciplined, is the missing piece a **semantic-component tier** (`BackButton`, `PlayControl`) between primitives and screens — the thing that would end the composition drift for good?
- Nothing is truly destructive server-side here. Would a **toast-with-undo** be both safer and lower-friction than the modal-confirm-everywhere pattern, and let the queue swipe-remove keep its speed?
- The queue is the emotional core of a music app — why is it the least-finished surface while auth is the most polished?
- Should a lint rule banning bare `TextInput` (and raw accent-as-text) turn these recurring drift classes into mechanical failures?
