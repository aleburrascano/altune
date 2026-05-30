# shared/ui — Altune design system (ADR-0008)

The single source of visual truth. Every screen composes these primitives and reads color via `useTheme()`; **never hardcode color, spacing, radius, or type**. Refreshed dark identity (ADR-0009): lifted-charcoal canvas (`#121214`), Cobalt accent (`#2D5BFF`), Plus Jakarta Sans (display, 700/600) + Inter (body), soft radius-16 surfaces (flat — no glow; active state is a 1px accent border), tasteful motion.

## Layout

- `theme/` — `palette.ts` (raw locked hex; do NOT consume directly) → `tokens.ts` (theme-independent spacing / radius / typography / motion) → `theme.ts` (the semantic `Theme` type + `ConfidenceLevel`) → `darkTheme.ts` / `lightTheme.ts` → `themes.ts` registry → `ThemeProvider.tsx` + `useTheme.ts`. `confidenceColor(theme, level)` maps discovery confidence to a semantic color.
- `primitives/` — `Screen, Text, Button, IconButton, Card, Row, Chip, Banner, ConfidenceDot, Artwork, Skeleton, Wordmark`. (`Banner` is currently unused — the discover provider-failure banner was removed in ADR-0009; kept for future status messaging.)
- `motion/` — `usePressScale` (RN `Animated` spring press feedback), `useReduceMotion` (gates animation off the OS setting).
- `navigation/` — `TabBar` (docked bottom bar for the `(tabs)` group: hairline top border, 2px active indicator, no glass blur). **Deliberately NOT re-exported from the top `index.ts` barrel**, so screens importing `@shared/ui` don't transitively pull `lucide-react-native` into jest.

## Conventions

- **Import from `@shared/ui`** (barrel = theme + primitives + motion). Components that render in jest (e.g. the auth screens) import primitives **directly** (`@shared/ui/primitives/Button`) to avoid loading `Artwork`→`expo-image` transitively.
- **`useTheme()` is the only color source.** The context default *is* `darkTheme`, so a component with no `ThemeProvider` mounted (bare-rendered tests) resolves to dark instead of throwing — load-bearing for the auth component tests.
- **Dark is the only v1 mode.** `lightTheme` is a drafted, inactive counterpart (satisfies the `.claude/rules/typescript-frontend.md` "every token has light + dark" rule); it is NOT visually tuned — don't ship light mode without a dedicated design pass.
- **Semantic colors stay off the brand accent** — cobalt always means "interactive", never "data" (confidence/warning/danger have their own roles).
- **Typography:** set `fontFamily` per weight (never `fontWeight`) so the `@expo-google-fonts` weighted families render without faux-bolding. Token `fontFamily` strings must match the font export names loaded in `app/_layout.tsx` (`PlusJakartaSans_700Bold`/`_600SemiBold` for display, `Inter_*` for body).

## Gotchas

- **Motion uses RN's built-in `Animated`** (NOT `react-native-reanimated`): zero native modules, so it runs in Expo Go. Reanimated 4's worklets TurboModule isn't present in Expo Go and crashed at startup — see ADR-0008.
- **jest:** `react-native-safe-area-context` is mocked in `apps/mobile/jest/setup-after-env.js` (`Screen` calls `useSafeAreaInsets`, which throws with no provider). RN `Animated` needs no mock.
- **Android system nav bar** is painted dark (canvas color + light buttons) in `app/_layout.tsx` on mount and on every `AppState` → `active`; the resume re-assert kills the white-flash bug under SDK 54 edge-to-edge (ADR-0009). `expo-blur` is still installed but no longer used by `TabBar`.
- **AIDEV-* anchors** in `theme/ThemeProvider.tsx`, `theme/lightTheme.ts`, and `navigation/TabBar.tsx` document the dark-only / mini-player-dock decisions — never strip them.

## Consumers

`features/auth`, `features/discover`, `features/library`, and `app/(tabs)/_layout.tsx`. Per the root CLAUDE.md promotion rule, an item earns a place here at **2+ feature consumers**.
