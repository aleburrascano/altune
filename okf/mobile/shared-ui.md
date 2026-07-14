---
type: Subsystem
title: Shared UI design system
description: Token-based theming (ADR-0008/ADR-0009), a semantic Theme contract with dark-only v1 mode, primitive components, and lightweight RN-Animated motion helpers.
resource: apps/mobile/src/shared/ui/
tags: [mobile, shared, ui, design-system, theming, motion]
verified_commit: 6a047a008fb23b38e719d9a9a3e9b539ab349d4d
---

The single source of visual truth (per `apps/mobile/src/shared/ui/CLAUDE.md`): every screen composes these primitives and reads color via `useTheme()` — no hardcoded color/spacing/radius/type. The token pipeline layers: `theme/palette.ts` (raw locked hex, not consumed directly) → `theme/tokens.ts` → `theme/theme.ts` (semantic contract) → `darkTheme.ts`/`lightTheme.ts` → `themes.ts` registry → `ThemeProvider.tsx`/`useTheme.ts`.

`theme/tokens.ts` holds theme-*independent* scales, shared across light/dark: `spacing` (4pt scale, `none`→`5xl`), `radius` (`sm`8/`md`12/`lg`16 for cards/`xl`24 for sheets/`full`999), `fontFamily` (keys must exactly match `useFonts`-registered names — `PlusJakartaSans_700Bold`/`_600SemiBold` for display, `Inter_*` for body; `fontFamily` is set per weight rather than `fontWeight`, avoiding faux-bolding), `typography` (a `Record<TypographyVariant, {...}>` — `displayXl`/`displayL`/`title`/`body`/`bodyStrong`/`label`/`caption`), `minInteractiveHeight` (48, WCAG AA), and `duration` (`fast`120/`base`200/`slow`320ms — "tasteful/minimal" motion personality).

`theme/theme.ts` defines the semantic `Theme = { scheme: 'dark'|'light', color: ThemeColors }` contract — every color is a *role* (`canvas`, `surface1/2`, `border`, `textPrimary/Secondary/Tertiary`, `accent`/`accentPressed`/`accentTint`/`onAccent`, `confHigh/Med/Low`, `warning`/`danger`/`success`, `heroGradient`), never a raw hue — and `ConfidenceLevel` (mirrors the wire `DiscoveryConfidence` without coupling the design system to `api-client`). `ThemeProvider.tsx`'s `ThemeContext` defaults to `darkTheme` (not undefined) — an AIDEV-NOTE-flagged deliberate choice so bare-rendered jest tests (no provider mounted) resolve to dark instead of throwing. **Dark is the only v1 mode**; `lightTheme` exists (satisfying the "every token has light+dark" rule) but is unvalidated visually — flipping `scheme` to a `useColorScheme()` read is the documented future path, requiring no component changes.

`primitives/index.ts` barrel-exports the full component set: `Text` (variant + semantic `tone` props, e.g. `TextTone = 'primary'|'secondary'|...|'danger'`), `Screen`, `Card`, `Row`, `Chip`, `Banner` (currently unused, kept for future status messaging), `ConfidenceDot`, `Artwork`, `Wordmark`, `IconButton`, `Button`, `Skeleton`, `SearchBar`, `ContextMenu`. Components that render under jest import primitives directly (bypassing the barrel) to avoid transitively pulling `lucide-react-native`/`expo-image`. `navigation/TabBar` is deliberately excluded from the barrel for the same jest-isolation reason.

`motion/index.ts` exports `usePressScale` (RN's built-in `Animated` spring press feedback — not Reanimated, since Reanimated 4's worklets TurboModule isn't present in Expo Go) and `useReduceMotion` (gates animation off the OS accessibility setting).

Consumers (2+ feature threshold met): [[auth-feature]], [[discover-feature]], [[library-feature]], `app/(tabs)/_layout.tsx` (see [[app-navigation]]).
