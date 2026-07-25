# Altune mobile (Expo + React Native + TS) — router

Vertical slices under `src/features/` — a feature owns its UI/hooks/api/tests end-to-end. Routes in `src/app/` (Expo Router, file-based, tabbed shell under `app/(tabs)/`); shared code in `src/shared/`, each with its own nested `CLAUDE.md`.

TS pattern vocabulary: **Read `~/.claude/lexicon/MANIFEST-ts.md` before proposing or rejecting any abstraction** (an `@`-import here does not expand — nested CLAUDE.md files load on demand, imports only expand at launch). Full entries under `~/.claude/lexicon/site/{path}/index.html` — Grep an entry for `Avoid|Cost` and quote its cost line when tradeoffs matter; never read a whole entry (~40k chars).

## Rules

Structure:

- Never import across features. Extraction to `shared/` requires 2+ real consumers.
- Use path aliases (`@/`, `@features/`, `@shared/`) over relative `../../` beyond one level.
- Navigate with `useRouter` from `expo-router`, and use the typed route paths.

Platform:

- Add native modules only via `npx expo install <name>`.
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
