# shared/ui — Altune design system (ADR-0008) — router

The single source of visual truth. Refreshed dark identity (ADR-0009): lifted-charcoal canvas (`#121214`), Cobalt accent (`#2D5BFF`), Plus Jakarta Sans (display) + Inter (body), soft radius-16 flat surfaces, tasteful motion.

Layout:

- `theme/` — `palette.ts` (raw locked hex) → `tokens.ts` (spacing / radius / typography / motion) → `theme.ts` (the semantic `Theme` type + `ConfidenceLevel`) → `darkTheme.ts` / `lightTheme.ts` → `themes.ts` → `ThemeProvider.tsx` + `useTheme.ts`; `confidenceColor(theme, level)`.
- `primitives/` — `Screen, Text, Button, IconButton, Card, Row, Chip, Banner, ConfidenceDot, Artwork, Skeleton, Wordmark, SearchBar`.
- `motion/` — `usePressScale`, `useReduceMotion`.
- `navigation/` — `TabBar`.

Consumers: `features/auth`, `features/discover`, `features/library`, `features/detail`, `app/(tabs)/_layout.tsx`. An item earns a place here at 2+ feature consumers.

## Rules

- Never hardcode color, spacing, radius or type — `useTheme()` is the only color source.
- Never consume `palette.ts` directly; go through the tokens and the semantic theme.
- Never re-export `Artwork`, `SearchBar` or `TabBar` from the barrel — import those directly by path.
- Never use the brand accent for data: cobalt means "interactive"; confidence/warning/danger have their own roles.
- Set `fontFamily` per weight, never `fontWeight`.
- Never use `react-native-reanimated` here — motion is RN's built-in `Animated`.
- Never ship light mode without a dedicated design pass; `lightTheme` is drafted, not tuned.

Why each rule exists: `okf/mobile/shared-ui.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
