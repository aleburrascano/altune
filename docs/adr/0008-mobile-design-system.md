# ADR-0008: Token-based mobile design system ("Midnight Studio")

- **Status:** Accepted
- **Date:** 2026-05-28
- **Deciders:** solo + Claude
- **Context tags:** [pattern, dependency]

> **Update (2026-05-28):** Motion was initially built on `react-native-reanimated` v4. It crashed at startup in **Expo Go** — Reanimated 4's `react-native-worklets` TurboModule is not present in the Expo Go runtime (`installTurboModule` throws). Because the chosen motion is minimal (press-scale + shimmer), we **dropped reanimated and reimplemented motion with React Native's built-in `Animated`** (zero native modules, Expo-Go-safe). The sections below reflect that final decision.

## Context

The mobile front-end was styled ad-hoc — inline `StyleSheet`, hardcoded hex values, and an inconsistent mix (dark main screens, light auth screens) — built only to exercise the shipped specs (`view-library`, `auth-integration`, `discover-music-v1`). `apps/mobile/src/shared/ui/` was empty. With more screens coming, every new screen would re-invent colors/spacing/type and drift further. We need one source of visual truth so the app has a consistent identity and a new screen is cheap to build correctly.

## Decision

Adopt a **token-based design system** in `apps/mobile/src/shared/ui/`, authored in **plain React Native `StyleSheet` + typed TypeScript token modules** (no styling library):

- **Theme** (`theme/`): raw `palette` → theme-independent `tokens` (spacing, radius, typography, motion) → a **semantic** `Theme` (`darkTheme` + a drafted `lightTheme`) consumed **only** via a `useTheme()` hook. v1 ships **dark only**; the light theme exists so light mode is a later config flip, not a rewrite. Semantic data colors (confidence high/med/low, warning, danger) are deliberately kept **off** the brand accent so indigo always means "interactive".
- **Identity:** near-black canvas `#0B0B0F`, one signature accent **Electric Indigo `#5B6CFF`** (+ violet→magenta hero gradient), Space Grotesk (display) + Inter (body), radius-16 "soft & spacious" surfaces, glow-based elevation (no hard shadows).
- **Primitives** (`primitives/`): `Screen, Text, Button, IconButton, Card, Row, Chip, Banner, ConfidenceDot, Artwork, Skeleton, Wordmark` — all consume `useTheme()`; none hardcode color.
- **Navigation:** a `(tabs)` route group with a **custom floating glass `GlassTabBar`** (Discover + Library), designed so a mini-player can dock above it and Settings/Profile can be added.
- **Motion:** "tasteful/minimal" — React Native's built-in `Animated` for press-scale feedback and skeleton shimmer; light `expo-haptics`. No reanimated, no shared-element transitions (Expo-Go-safe by design).
- **New dependencies:** `@expo-google-fonts/{space-grotesk,inter}` + `expo-font` + `expo-splash-screen`, `lucide-react-native` (+ `react-native-svg`), `expo-blur`, `expo-image`, `expo-haptics`, `expo-linear-gradient`. (No `react-native-reanimated` — see the Update note.)

The three shipped screens were retrofitted onto the system with **every `testID` and behavior preserved** (sacred tests stayed green).

## Alternatives considered

| Alternative | Why not |
|---|---|
| **Unistyles v3** | Purpose-built for themed design systems (variants, breakpoints, fast C++ core), but adds a dependency + new-arch coupling we don't need for a dark-only v1. Revisit if theming/variant pain appears. |
| **NativeWind (Tailwind)** | Fast utility DX, but class-string styling, extra build setup, and verbose for complex components. Not worth the toolchain for a small custom kit. |
| **Tamagui** | Powerful (compiler, tokens, animations) but heavy, opinionated, and a steep curve — overkill for a focused custom system. |
| **Keep ad-hoc inline styles** | This *is* the problem: no consistency, every screen re-invents the look, dark/light drift. |
| **react-native-reanimated (v4)** | Initially adopted, then **dropped** — its worklets TurboModule isn't in Expo Go and crashed at startup. RN's built-in `Animated` covers the minimal press/shimmer motion with zero native modules. Revisit only for richer gesture-driven motion, and then via a dev build. |

## Consequences

### What becomes easier
- Every screen consumes one palette/spacing/type scale → consistent identity for free.
- A new screen composes primitives instead of re-deriving styles.
- The look changes in one place; a light theme drops in by activating the existing `lightTheme`.

### What becomes harder
- Every component must read color via `useTheme()` (enforced by the `ux-reviewer` / `.claude/rules/typescript-frontend.md`), never hardcode — a discipline cost.
- A few new native deps + one jest mock (safe-area-context) to keep component tests rendering.

### What we're committing to (and the cost to reverse)
- **Plain StyleSheet + tokens** as the styling approach. Swapping to a library later means rewriting primitive internals (but **not** screens — they only touch primitives + `useTheme`, so the blast radius is contained by design).
- **Motion via RN `Animated`** (no native motion dependency, so it runs in Expo Go). If richer motion is ever needed, adopting `react-native-reanimated` would require a dev build (it does not run in Expo Go) and would touch only the helpers in `shared/ui/motion/` — screens are unaffected.

## Implementation notes

- **Babel:** no motion plugin needed — `babel.config.js` keeps only the `module-resolver` alias plugin.
- **Jest:** `jest/setup-after-env.js` maps `react-native-safe-area-context` to its official jest mock (the `Screen` primitive calls `useSafeAreaInsets`, which throws with no provider). RN `Animated` needs no mock.
- **Fonts:** loaded in `app/_layout.tsx` via `useFonts` + `SplashScreen.preventAutoHideAsync()`; the `fontFamily` token strings must match the `@expo-google-fonts` weight export names.
- **`useTheme` fallback:** the context default *is* `darkTheme`, so components rendered without a `ThemeProvider` (the bare-rendered auth screens in jest) resolve to dark instead of throwing.
- **Android `expo-blur`:** `GlassTabBar` branches on `Platform.OS` — Android falls back to a solid `surface2` bar (BlurView is unreliable there).
- **Deferred:** actual app-icon / splash-screen *artwork* is a separate creative task; only the `Wordmark` primitive + the font-load splash hold ship here.

## Vault references

- [vault: wiki/concepts/Vertical Slice Architecture.md] — the design system lives in `shared/ui` (legitimate: 3+ consumers per the 2-consumer rule); screen-specific composition stays in each feature slice.
- [vault: wiki/concepts/Modularity.md] — the system is a cohesive module with a narrow public interface (`@shared/ui`): tokens → primitives → screens, each understandable and replaceable independently.
- Note: the software-architecture-design vault returned **no dedicated "design tokens / design system" note**; this ADR rests on the architecture-organization notes above plus the project's own `.claude/rules/typescript-frontend.md` theming rule.

## Related

- Predecessor ADRs: `docs/adr/0005-mobile-server-state-react-query.md` (root providers), `docs/adr/0006-supabase-auth.md` (AuthGate), `docs/adr/0007-unified-music-search.md` (discover surface).
- Plan: `~/.claude/plans/hey-so-these-past-iterative-micali.md` (design brainstorm + build plan).
- Specs retrofitted: `docs/specs/{view-library,auth-integration,discover-music-v1}/spec.md` (testIDs preserved).
