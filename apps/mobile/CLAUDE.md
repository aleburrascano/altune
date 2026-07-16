# Altune mobile (Expo + React Native + TS) — local rules

Vertical slices under `src/features/` — a feature owns its UI/hooks/api/tests end-to-end. No cross-feature imports; extraction to `shared/` requires 2+ real consumers. **Rest of this file: Expo / RN platform quirks.**

TS pattern vocabulary: **Read `~/.claude/lexicon/MANIFEST-ts.md` before proposing
or rejecting any abstraction** (an `@`-import here does not expand — nested
CLAUDE.md files load on demand, imports only expand at launch). Full entries under
`~/.claude/lexicon/site/{path}/index.html` — Grep an entry for `Avoid|Cost` and
quote its cost line when tradeoffs matter; never read a whole entry (~40k chars).

## Stack

- Expo SDK 54 with new architecture enabled (`newArchEnabled: true` in `app.json`).
- Expo Router (file-based routing, `app/` directory); tabbed shell under `app/(tabs)/`.
- React 19, React Native 0.81.
- Design system: token-based `shared/ui` (ADR-0008) — plain `StyleSheet` + typed tokens + `useTheme`, `react-native-reanimated` for motion. Dark-only in v1.

## Routing

- Routes live in `src/app/`. File-based: `app/index.tsx` is `/`, `app/library/[id].tsx` is `/library/:id`, etc.
- Layouts in `_layout.tsx`. Wrap with providers (theme, react-query, error boundary) in the root `_layout.tsx`.
- Navigate via `import { useRouter } from 'expo-router'` — declarative.
- `typedRoutes: true` is on; paths are typed. Use them.

## Path aliases

Both `tsconfig.json` and `babel.config.js` are configured:
- `@/foo` → `src/foo`
- `@features/<name>/...` → `src/features/<name>/...`
- `@shared/...` → `src/shared/...`

Use aliases over relative `../../` imports beyond one level.

## Native modules

- Add only via `npx expo install <name>` (gets the compatible version for your SDK).
- Adding native module → may require `expo prebuild` if not in the managed pre-built clients. Document in an ADR when this happens.
- Test on both iOS and Android (or document why one is deferred).

## Performance defaults

- Lists: `FlatList` / `SectionList` / `FlashList` (if perf demands).
- Images: `expo-image` for caching + remote loading.
- Animations: `react-native-reanimated` for anything beyond `Animated` basics.
- Heavy work: web workers / native modules — never block the JS thread on UI events.

## State

- **Server state**: React Query (when added; not in scaffold).
- **Local state**: `useState`, `useReducer`, context for cross-tree.
- **Global state library** (Zustand/Jotai/etc.): NONE without an ADR.

## Storage

- **Sensitive** (tokens, secrets): `expo-secure-store`.
- **Non-sensitive**: `expo-sqlite` for structured, `AsyncStorage` for k/v.
- **Never** store secrets in `AsyncStorage`.

## Testing

- Jest + `jest-expo` preset + `@testing-library/react-native`.
- Unit/component tests live next to source in `__tests__/`.
- E2E (when added): Maestro preferred over Detox (lighter setup).

## Debugging

- React DevTools (Chrome inspector or standalone).
- `react-native-flipper` if needed (network, AsyncStorage inspection).
- `__DEV__` global for dev-only code paths.

## Anti-patterns

- `console.log` in committed code (use `console.warn`/`console.error` if needed; better: structured logger when added).
- `setTimeout` for layout work (use `requestAnimationFrame` or `InteractionManager`).
- Inline styles that should be theme tokens.
- Class components.
- Native modules from React Native packages that aren't Expo-compatible without checking `expo install` resolves them.

## Knowledge base

`okf/mobile/index.md` indexes the curated concept docs for every feature and shared subsystem — read the relevant one before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
