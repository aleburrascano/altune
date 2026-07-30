# Altune mobile (Expo + React Native + TS) — router

Vertical slices under `src/features/` — a feature owns its UI/hooks/api/tests end-to-end. Routes in `src/app/` (Expo Router, file-based, tabbed shell under `app/(tabs)/`); shared code in `src/shared/`, each with its own nested `CLAUDE.md`.

Test harness: `jest.config.js` (per-glob coverage floors, raise-only), `jest/setup-env.js` + `jest/setup-after-env.js`, native doubles in `jest/doubles/` (`expo-file-system`, `expo-secure-store`, `react-native-track-player`), and `__tests__/harness.test.ts` which constrains the doubles themselves. There is no `e2e/` directory and no `__mocks__/` directory — both were removed on 2026-07-30.

TS pattern vocabulary: **Read `~/.claude/lexicon/MANIFEST-ts.md` before proposing or rejecting any abstraction** (an `@`-import here does not expand — nested CLAUDE.md files load on demand, imports only expand at launch). Full entries under `~/.claude/lexicon/site/{path}/index.html` — Grep an entry for `Avoid|Cost` and quote its cost line when tradeoffs matter; never read a whole entry (~40k chars).

## Rules

Structure:

- Never import across features. Extraction to `shared/` requires 2+ real consumers.
- Reach a feature's cache from `shared/` only through a registration seam — `@shared/acquisition/audioCacheInvalidation` is the one for on-disk audio.
- Use path aliases (`@/`, `@features/`, `@shared/`) over relative `../../` beyond one level.
- Navigate with `useRouter` from `expo-router`, and use the typed route paths.
- Read grouped, filtered and sorted collections from the API; never re-derive them on the device (`@shared/api-client/library`).
- Read a track's ownership from the server stamp on a result, overlaid with `@shared/acquisition/trackStatusStore` for liveness; never hold the whole library to answer it.
- Never render server-mutable state from a snapshot (a route param, a module handoff, a value computed once in a parent) — subscribe to the query cache or the owning store so the screen updates in place.
- Every server event the backend publishes is declared in `@shared/events/eventTypes` and handled in `applyServerEvent`; an unhandled type is a compile error, not a silent no-op.
- A component that displays server-mutable state gets a liveness test: mutate the store, assert the rendered output changed.

Platform:

- Prettier config lives in `package.json`'s `prettier` key — never add a `.prettierrc*` file back.
- Keep CI workflows in the repo-root `.github/workflows/` — GitHub ignores a nested `.github/`, so one here is dead config that looks alive.
- Add native modules only via `npx expo install <name>` — but pin `@testing-library/react-native` to 13.x by hand; `expo install` resolves non-Expo packages unpinned and 14.x does not work under the SDK 54 preset.
- Give every native double in `jest/doubles/` a reachable failure mode, and make every write observable by a read.
- Mutation-test a slice with `npm run mutate`; raise `thresholds.break` in `stryker.config.json` when a slice is hardened, never lower it.
- Keep `.fallowrc.json`'s `boundaries.rules` identical to the two import rules above — a feature reaches only `shared`, and `shared` reaches no feature.
- Gate fallow with `--fail-on-issues`; its exit code ignores rule severity, so `error` findings alone do not fail it.
- Lower the `MAX` ceilings in `test-mobile.yml` (fallow complexity, react-doctor warnings) as the counts fall; never raise one to make CI green.
- Run the pinned `npm run doctor:ci` in CI, never `npx react-doctor@latest` — a gate must not change behaviour because upstream shipped a new rule.
- Keep Stryker's sandbox outside `rootDir` (`tempDirName` is `../../.stryker-tmp`) — inside `apps/mobile`, jest collects the sandbox's own test copies and silently doubles the suite.
- Test on both iOS and Android, or document why one is deferred.
- Never block the JS thread on UI events — push heavy work to workers or native.
- Never store secrets in `AsyncStorage`; use `expo-secure-store`.
- Never add a global state library without an ADR.

Never ship:

- `console.log` in committed code.
- `setTimeout` for layout work — use `requestAnimationFrame` or `InteractionManager`.
- Inline styles that should be theme tokens.
- Class components.
- A React Native package that `expo install` doesn't resolve.

Why each rule exists, and the platform baseline (SDK version, aliases, chosen libraries, testing and debugging setup): `okf/mobile/index.md` — it indexes the concept doc for every feature and shared subsystem. Read the relevant one before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
