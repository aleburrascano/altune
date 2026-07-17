---
target: entire frontend
total_score: 27
p0_count: 0
p1_count: 2
timestamp: 2026-07-17T22-55-14Z
slug: apps-mobile-entire-frontend
---
Method: dual-agent (A: ae2802f0 · B: a5cc0c0c). Target: Altune mobile — entire frontend (post commits f028229/0fcb079/e5ed4d7/6e9332b). Register: product / native mobile. Detector: HTML/CSS-only, not applicable to RN (attempted, exit 0, empty).

## Design Health Score

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | Skeletons, pending bar, acquisition lifecycle strong; no offline indicator. |
| 2 | Match System / Real World | 3 | Familiar transport; undercut by "Resolve featured artists" dev jargon and the Song/Track split. |
| 3 | User Control and Freedom | 3 | Back fallbacks, confirms, queue swipe/reorder/clear; Sign Out unconfirmed. |
| 4 | Consistency and Standards | 2 | Song vs Track noun; two overflow surfaces (ContextMenu popover vs ActionSheet); 4 bespoke play buttons; 44/48 target token split. |
| 5 | Error Prevention | 3 | Validation-gated submit, disabled states, destructive confirms, artist-invariant Save guard. |
| 6 | Recognition Rather Than Recall | 3 | Chips, recent searches, visible sort; repeat mode is icon-only. |
| 7 | Flexibility and Efficiency | 3 | Haptics, search history, enter-to-commit, queue swipe; no long-press shortcuts. |
| 8 | Aesthetic and Minimalist Design | 3 | Restrained, decluttered; Settings maintenance affordance breaks the minimalism. |
| 9 | Error Recovery | 3 | Retry across discover/library/player/row; mostly human copy. |
| 10 | Help and Documentation | 1 | Essentially none — no onboarding, first-run, or about/help. |
| **Total** | | **27/40** | **Competent (upper-middle). Anchored by consistency (4) and help (10).** |

## Anti-Patterns Verdict

**Clean bill — not AI slop.** Genuinely crafted token system with documented accessibility tuning. The deterministic pass found **zero** substantive violations and independently re-verified every fix from the prior two rounds.

**Regression check — all prior fixes hold (verified by evidence):**
- `accentText` #6B8FFF measures **6.24 / 5.66 / 5.21:1** — the accent-as-text contrast fix works; `textTertiary` #8C8C96 passes (5.62/5.10/4.70); **no body-text pair below 4.5:1** anywhere.
- `TextField` covers focus + `error` danger border + secure show/hide + a11y-label fallback; 3 auth screens + CreatePlaylist routed through it.
- All 5 modal/overlay dismiss backdrops are labeled; `IconButton` requires a label and now auto-dims disabled.
- `useReduceMotion` honored at the 2 real timed-animation sites; the only other reanimated use is gesture-follow.
- Loading idiom is now **consistent** — skeletons everywhere; the only `ActivityIndicator`s left are defensible inline action spinners (save glyph, row retry, "Searching…").
- Queue reorder present (accessible ActionSheet menu); orphaned `PlayIconButton` removed; `TouchableOpacity` count is 0.

## Overall Impression

Three passes in, the app is a solid, above-median 27 — and the number is honest, not stuck. Every backlog fix landed and verified; the score plateaus because the remaining ceiling is **product / IA decisions, not polish**. Two heuristics anchor it: **Consistency (2)** — the same object is named two things, the same "track options" action opens two different menu types, and the most important control (play) is built four ways; and **Help (1)** — there is no onboarding or about layer at all. Neither moves with a token tweak; both need a product ruling.

## What's Working

1. **Token/theme rigor with documented AA tuning** — the palette lightens tertiary and accent-as-text specifically to clear 4.5:1 on dark, and reserves cobalt for "interactive" only. Most apps never separate these.
2. **Accessibility depth that serves a real screen-reader user** — `adjustable` scrubber with ±15s actions and live value, required icon labels, acquisition state encoded into row a11y labels, reduce-motion gating. Top-decile for an indie app.
3. **State coverage + teaching empty states** — Discover's 5-state machine, `LibraryNoResults` distinguishing "filtered" from "gone", inline acquisition lifecycle, geometry-matched skeletons.

## Priority Issues

**[P1] "Song" vs "Track" vocabulary split across the two primary tabs.**
- Why it matters: `kindLabel('track')` returns "Song"/"Songs" (`discover/state.ts:123`) so Discover's chip, Top Result, and zero-state say "Song", while Library says "Track". The project's own `docs/ubiquitous-language.md` states "Song is banned — the noun is Track." The same object is named two things as you tab-switch — the exact earned-familiarity crack, and it contradicts the codebase's stated domain rule on the user-facing surface. (Flagged as a "conscious ruling needed" item since the first critique.)
- Fix: make one ruling. If "Track" is the product term, `kindLabel('track')` returns "Track"/"Tracks". One function. If "Song" is a deliberate consumer-facing choice, update `ubiquitous-language.md` so code and doc agree.

**[P1] Settings leaks a maintenance tool and lacks real IA.**
- Why it matters: the prominent control is "Resolve featured artists → Updated 3 of 50 tracks" (`SettingsScreen.tsx`) — internal telemetry a user can't act on; reads as a debug leak, and it's the app's emotional low. No account/playback/about/version sections; Sign Out is an unconfirmed low-emphasis ghost. (The round-1 regroup helped hierarchy but didn't resolve the "why is this here" problem.)
- Fix: move maintenance behind a dev/hidden section or remove it; give Settings real sections (Account / Playback / About+version); confirm sign-out.

**[P2] Four bespoke play buttons + two overflow-menu paradigms — the design system isn't the source of truth for the two most-used interactions.**
- Why it matters: play renders 4 ways (FullPlayer 64pt, detail 50pt, PlaylistHero pill, Queue badge); "track options" opens a ContextMenu popover on Library/Playlist rows but an ActionSheet on Queue rows. Same action, different component. (Note: the queue's ActionSheet is new this session — the reorder menu — so the overflow split partly traces to that addition.)
- Fix: decide one track-options surface app-wide; and either promote a shared play-control or consciously document the per-context differentiation (the play controls do differ by state/chrome — the honest answer may be "document, don't merge", but the overflow menus genuinely should unify).

**[P2] Centered modals have no keyboard avoidance.**
- Why it matters: `CreatePlaylistModal` (centered card, autofocus input) and `PlaylistHero` inline rename have no `KeyboardAvoidingView`; on Android / short screens the keyboard can cover the field and actions. The auth layout already solves this — reuse it.

**[P2] One raw `TextInput` remains + a few primitive-state gaps.**
- Why it matters: `PlaylistHero.tsx:59` inline rename still bypasses `TextField` (reimplements border/focus). `Chip` and `SearchBar` have no `disabled` state; `TextField`/`Chip` no disabled. Minor but the input-consolidation is 95%, not 100%.

## Persona Red Flags

**Casey (one-handed):** repeat mode is icon-only (Repeat vs Repeat1) — mode unreadable at a glance; mini-player bar navigates to the full player while play/skip live inside it — a near-miss opens the player instead of pausing; Library's stacked top chrome pushes chips to a thumb stretch.

**Sam (accessibility — best-served):** repeat button announces the raw enum "Repeat: all/one/off", not natural language; player status transitions ("Preview ended", "Finished") and row acquisition changes aren't in a live region, so they aren't announced on change.

**Alex (power user):** no long-press row shortcuts (every action is ⋮ → menu); shuffle/repeat live only in the full player; no result count on Discover.

## Minor Observations

- `minInteractiveHeight` token is 48 but `IconButton`/`Chip` use 44 — a silent token split; pick one.
- Chips are tall/chunky (`paddingVertical: md` + `minHeight: 44`).
- `PlaylistHero.tsx:123` `boxShadow` uses a hardcoded rgba (no shadow token exists) — the one real color-literal; consider a shadow token.
- `ActionSheet` uses `borderTopRadius: 20` off the radius scale (should be `radius.xl` = 24).
- `Banner` primitive still documented as unused (it's actually used by auth) — stale doc note.
- Library has no pull-to-refresh (Discover does); relies on background polling.

## Questions to Consider

- If the domain bans "Song," why does the flagship discovery surface say "Songs"? Does the user want your ontology, or one word for the thing they tap?
- Should "Resolve featured artists / Updated 3 of 50" exist on an end-user Settings screen at all — is Settings built for the user or the developer?
- Same "track options" opens a popover on one screen and a bottom sheet on another. Which is the app's menu, and why does the other exist?
- Where is the peak-end reward? Saving your first track and creating your first playlist are silent — what makes a self-hosted library feel *earned*?
- The score has held at 27 across three passes while every fix landed. Is the real lever now a product decision (one noun, a help layer, unified affordances) rather than another polish round?
