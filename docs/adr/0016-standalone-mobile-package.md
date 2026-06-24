# ADR-0016: `apps/mobile` is a standalone npm package, not an npm-workspace member

- **Status:** Accepted
- **Date:** 2026-06-24
- **Deciders:** solo + Claude
- **Context tags:** [arch, tooling]

## Context

ADR-0001 set the `apps/` + `services/` folder layout and mentioned, as an
implementation detail, a "root `package.json` with workspaces." In practice the
repo has exactly **one** JavaScript package — `apps/mobile` (the Go service under
`services/go-api` has no `node_modules`). The npm workspace therefore existed
mostly to host repo-level git tooling (husky + commitlint) at the git root.

The workspace's default **dependency hoisting** pulled mobile's deps up into the
root `node_modules`, while `apps/mobile/.npmrc` carried an
`install-strategy=nested` **counter-hack** to drag `jest-expo` + `react-native`
back down so jest-expo's internal `require('react-native/jest-preset')` could
resolve. This half-hoisted / half-nested split caused real breakage:

- `tsc --noEmit` reported **1000+ errors** because TypeScript's automatic global
  `@types` inclusion didn't apply `@types/jest` reliably across the split tree
  (test globals `describe`/`it`/`expect` unresolved).
- `fallow`'s import resolver mis-flagged dozens of exports/types as "unused"
  because it couldn't follow imports across the two `node_modules` trees.
- The `.npmrc` nested hack was itself fragile and self-documented as such.

The folder layout from ADR-0001 is **not** in question — only the npm
dependency-management mechanism.

## Decision

Decouple `apps/mobile` into a **standalone npm package**:

- **Root `package.json`** keeps only repo-governance tooling (husky +
  commitlint). No `workspaces` field; no app dependencies. Git hooks live next
  to `.git`, which is the repo root — so this thin manifest stays.
- **`apps/mobile`** owns the full JS dependency surface: its own
  `package-lock.json` and one flat, self-contained `node_modules`. The
  `react-native-web` dep and the `react-native-worklets` `overrides` pin moved
  here from the root.
- **`apps/mobile/.npmrc`** keeps only `legacy-peer-deps=true` (the standard RN
  install pragma — the RN/Expo ecosystem ships tight peer ranges). The
  `install-strategy=nested` hack is **removed**; with no workspace there is no
  hoisting to fight.
- TypeScript test globals are made deterministic with an explicit
  `compilerOptions.types: ["jest", "node"]` in `apps/mobile/tsconfig.json`
  rather than relying on environment-dependent automatic `@types` inclusion.

## Consequences

### What becomes easier
- One flat `node_modules`: `@types/jest`, `jest`, `react-native`, `typescript`
  all colocated → `tsc` resolves test globals, `fallow` resolves imports
  accurately, jest-expo finds its preset with no hack.
- ESLint, Jest, tsc, and fallow all run green from `apps/mobile`.
- Mobile is now a conventional standalone Expo app — what EAS and Expo tooling
  expect by default.

### What becomes harder
- **Two installs**: `npm install` at the root (husky/commitlint) and
  `npm install` in `apps/mobile` (the app). A future CI must run both.
- The root and mobile dependency trees no longer share a lockfile (acceptable —
  they share nothing meaningful).

### What we're committing to
- `apps/mobile` self-manages its deps. A second JS package (e.g. a web client)
  would be its own standalone package too, or would justify revisiting a
  workspace **then** — not pre-emptively.

## Supersedes / amends

- Amends the one-line "package.json with workspaces" implementation note in
  **ADR-0001**. ADR-0001's folder-layout decision (`apps/` + `services/`)
  stands unchanged.

## Related

- ADR-0001 — monorepo layout
- `apps/mobile/jest.config.js` — docstring on why the preset resolves cleanly now
- `apps/mobile/eslint.config.js` — flat config (ESLint 9 + eslint-config-expo)
