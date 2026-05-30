# ADR-0009: Visual refresh — Cobalt on lifted charcoal, docked tab bar

- **Status:** Accepted
- **Date:** 2026-05-30
- **Deciders:** solo + Claude
- **Context tags:** [pattern, dependency]

> Supersedes the **visual identity** decisions of `docs/adr/0008-mobile-design-system.md` (the
> "Midnight Studio" palette, typeface, glow elevation, and floating glass tab bar). The *architecture*
> of ADR-0008 — token-based system, plain `StyleSheet`, `useTheme()` as the only color source,
> primitives composed by screens — is **unchanged** and still in force.

## Context

The token system from ADR-0008 was sound, but the applied look read "vibe-coded" rather than sleek.
Through a visual brainstorming session (live mockups), three concrete problems surfaced: the display
typeface (**Space Grotesk**) read overtly "techy"; the **floating glass tab bar** with an `accentTint`
pill behind the active icon looked amateur; and the Android **system** navigation bar flashed white on
resume because it was never painted to match the dark UI. The accent (`#5B6CFF` Electric Indigo) also
leaned purple, reinforcing the techy feel. Because every screen reads color/type via tokens, a re-skin
was cheap — the fix is a token swap plus a few targeted component changes, not a rewrite.

## Decision

Refresh the dark identity and fix the nav bar:

- **Typeface:** display swaps **Space Grotesk → Plus Jakarta Sans** (700/600); body stays **Inter**.
  Display variants gain `letterSpacing: -0.5`.
- **Accent:** Electric Indigo `#5B6CFF` → **Cobalt `#2D5BFF`** (purple removed). Hero gradient becomes
  cobalt → `#5B82FF`; magenta is dropped.
- **Base:** pure black `#0B0B0F` → **lifted charcoal `#121214`**; surfaces and text tiers re-toned to match.
- **Elevation:** the soft accent **glow is removed** (`glowStyle` deleted, `accentGlow` token dropped);
  the active `Card` state is now a 1px accent border.
- **Tab bar:** the floating glass `GlassTabBar` becomes a **docked `TabBar`** — flush to the bottom edge,
  hairline top border, no pill; the active tab is marked by a 2px accent indicator. `expo-blur` is no
  longer used by it.
- **Android system nav bar:** `expo-navigation-bar` paints it to the canvas color with light buttons on
  mount **and** on every `AppState` → `active`, eliminating the white-on-resume flash (edge-to-edge, SDK 54).
- **Discovery:** the provider-failure banner (`PartialBanner`) is **removed** — a failed upstream source
  is not actionable for the user; partial results already render normally.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Keep Space Grotesk, only retune sizes | The typeface itself was the "techy" signal; sizing tweaks don't address it. |
| Brighter accent (Sky/Cyan) | Luminous on black but tiring on fills; Cobalt keeps the existing identity with less purple. |
| Keep the floating glass bar, drop only the pill | User found the *float itself* off; docking reads cleaner and removes the Android blur fallback. |
| `androidNavigationBar` in app.json | Ignored under SDK 54 edge-to-edge; runtime `expo-navigation-bar` + resume re-assert is the working path. |
| Keep the provider banner | Surfacing an upstream outage the user can't act on is noise; results still render without it. |

## Consequences

### What becomes easier
- The app reads intentional/premium; type + cobalt + charcoal cohere.
- One fewer native quirk (no glass-blur Android fallback; no white nav-bar bug).

### What becomes harder
- `expo-navigation-bar` is a new dependency to keep SDK-compatible.

### What we're committing to (and the cost to reverse)
- Cobalt-on-charcoal + Plus Jakarta as the v1 identity. Reversing is again a token swap (contained to
  `theme/` + the few components touched here) — the ADR-0008 architecture keeps the blast radius small.

## Implementation notes

- New dep: `expo-navigation-bar`; dropped dep: `@expo-google-fonts/space-grotesk`; added font:
  `@expo-google-fonts/plus-jakarta-sans`. `expo-blur` remains installed but unused.
- `GlassTabBar.tsx` renamed to `TabBar.tsx`; the `Banner` primitive remains (now unused — left in place
  rather than deleted, per the surgical-changes rule).
- Verified: `tsc` clean, eslint/prettier clean, 66/66 jest green (the `_shouldShowPartialBanner` cases
  were removed with the banner).

## Vault references

- [vault: wiki/concepts/Modularity.md] — the re-skin stays inside the `@shared/ui` module's narrow
  interface; screens consume tokens and were largely untouched.
- Note: the software-architecture-design vault has no dedicated "design tokens / visual identity" note;
  this rests on the same modularity grounding as ADR-0008 plus `.claude/rules/typescript-frontend.md`.

## Related

- Supersedes (visual specifics only): `docs/adr/0008-mobile-design-system.md`.
- Plan: `~/.claude/plans/hey-so-i-just-clever-moon.md` (visual brainstorm + build plan).
