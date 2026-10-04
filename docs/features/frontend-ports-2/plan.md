# Frontend ports, slice 2: discover, settings and auth

## Outcome
Same outcome as epic #1127: feature logic in `apps/mobile` stops importing HTTP calls directly, tests can run on a fake port, and pure rules live in `domain/`. Library is done. This slice carries the three small features, discover, settings and auth, onto the template in `docs/conventions/feature-layers.md`. Done, to the developer: discover and auth have zero MC-8 hits, settings has only the feedback calls that #376 deletes, and a test keeps every feature's port boundary. The `domain/` moves for these three wait for the alias codemods (#1056–#1058), so imports are rewritten once, not twice.

## Appetite
4 tickets, 4 waves (a chain: each shares MC-8's baseline line with the next). Cap 8, each about an hour of build.

## Scope
### In
- **Discover port.** `features/discover/ports/discoverPort.ts` with `searchDiscovery`, `suggestDiscovery` and `listSearchHistory` (`shared/api-client/discovery.ts:258,289,304`), each member typed `typeof` the api-client function. Plus `adapters/http/httpDiscoverPort.ts` (object literal of arrows), `ports/DiscoverPortProvider.tsx` (context, HTTP default), `__tests__/memoryDiscoverPort.ts` and a `describe.each` contract test. The three hooks move onto it: `useDiscoverSearch.ts:4` (its plain factory `searchQueryFn(queryClient, …)` at `:40` takes the port as an argument), `useAutocompleteSuggestions.ts:4`, `useSearchHistory.ts:3`. `useClearSearchHistory.ts:1` imports `isRetryable` from `@shared/errors` instead of the barrel.
- **Settings port.** `features/settings/ports/settingsPort.ts` with `backfillFeaturedArtists` (`tracks.ts:222`), the same four pieces, and `useBackfillFeatured.ts:3` on it. The plain recursive `backfillFrom` (`:15`) and the module const `backfillWholeLibrary` (`:26`) take the port as an argument. MC-2's owner for `backfillFeaturedArtists` moves to the adapter, the same way #1128 and #1132 did it, with a path-aware test like `lintMc2At`. `failureCopyForAction.ts:1` and `failureCopyForReport.ts:1` import `ApiError` and `NetworkError` from `@shared/errors`.
- **Auth adapters.** Auth's two api-client imports are both `apiBase` for raw `fetch` (`reportSignInFailure.ts:1,10`, `testAuth.ts:3,52`). Both files are adapters already, so they move to `features/auth/adapters/http/`. In the same PR: MC-1's two exact ignore paths (`mc-1-network.js:13-14`) follow them, else MC-1 jumps from 9 to 11. The `testAuth.ts` entries in `apps/mobile/fallow-baseline.json:33-35` follow too, since CI runs fallow with `--fail-on-issues` (`ci.yml:182`). The importers `useOAuth.ts:18`, `useOAuth.web.ts:13`, `useSignIn.ts:5`, `TestAuthBridge.tsx:3` and two test files change paths, and the test edits go through test-amend. Auth gets no port: its real dependency is Supabase, and a port would only wrap `supabase.auth`.
- **MC-8 baseline.** It is an exact count (baseline at `eslint/consistency/mc-8-feature-ports.js:16`, equality check at `scripts/consistency-ratchet.mjs:73`), so each of the three tickets above lowers it in the same PR: 32 → 28 (discover) → 25 (settings) → 23 (auth). That one line is shared, so the three run in a chain. The ratchet test also pins the count at `scripts/__tests__/consistency-ratchet.test.ts:168,172` (`MC-8: 32 / baseline 32`), so each ticket moves that pin too, through test-amend.
- **One cross-feature port invariants test**, `src/features/__tests__/featurePortInvariants.test.ts` (new folder, `*Invariants` names pass test-home), asserts that library, discover and auth make no api-client value import outside `adapters/` (allow-list as MC-8), and that settings makes none besides the feedback files.
- `docs/conventions/feature-layers.md` lists the three features and the auth exception (adapters, no port), notes that auth's adapters also import Supabase, the device module and the kill switch (an exception to the table's "may import" column), and states the purity rule plainly: the domain check reads direct imports only, so a file whose imports pull in the platform one step down, or that registers a listener when loaded, stays out of `domain/`.

### Out
- The `domain/` moves for discover, settings and auth. The red-team found that they collide with fallow's path-keyed baseline (`fallow-baseline.json`, every move rewrites it) and with the alias codemods #1056–#1058, which rewrite the same import lines. Doing them after the codemods means one rewrite. Some "pure" files aren't pure either: `completeAuthIntent` calls Supabase and imports the React-using `recoveryUnlock`, `authRedirect` and `parseAuthLink` pull in `@shared/device/device`, `downloadStatsModel` pulls in the file store, and `searchState` registers a sign-out listener when loaded. Backlog.
- `submitReport` and `makeReportIdempotencyKey` in settings. #376 deletes the whole feedback feature, so porting them is waste. They stay as MC-8's last 2 settings hits.
- `reportDiagnostics` (impure one step down through `@shared/device/device`), `dangerZoneActions` (lucide icons) and `recoveryUnlock` (react): split pure from UI first. Backlog.
- Detail and playback (21 MC-8 hits) and the shared modules: their own slices.
- Renaming `apps/mobile`: it touches CI, deploy workflows, lane tooling under `~/.claude` and the acceptance folders, so it is its own ticket, run alone.

## Pre-mortem
1. It failed because the three port tickets each changed MC-8's baseline in parallel PRs, and every merge after the first went red on the exact-count ratchet.
2. It failed because moving the auth files left fallow's path-keyed baseline and MC-1's exact ignore paths pointing at the old paths, and CI went red on main.
3. It failed because discover's port ticket and #1021 (rename of the two `useClearSearchHistory` hooks) edited the same files at once.

## Risks
- **Baseline races (pre-mortem 1).** The three port tickets form a chain (discover → settings → auth), and each names its exact new baseline in Done when.
- **Stale paths (pre-mortem 2).** The auth ticket edits MC-1's ignores and `fallow-baseline.json` in the same PR, and its Verify runs the ratchet and `npx fallow --fail-on-issues`. The alias codemods #1056–#1058 lock their whole feature, so the file-family locks keep them from running beside these tickets.
- **#1021 (pre-mortem 3).** Discover's port ticket depends on #1021, which is ready now. It is also ordered against #1068, which edits `useAutocompleteSuggestions.ts`.
- **fallow is red on main.** #1020, #1021 and #1022 block `npx fallow --fail-on-issues`, and #1020 edits `fallow-baseline.json`. The auth ticket depends on all three.
- **Auth is the sign-in path.** Moving `reportSignInFailure.ts` and `testAuth.ts` is a path move with no logic change. Its tests and the MC-1 ratchet (baseline 9) must pass unchanged. It carries the `risk` label, so it gets the attack pass.
- **Test edits.** The auth move forces import-path edits in two test files. As in #1134 and #1135, the builder reports them and test-amend applies them.

## Build
Extends the template from epic #1127 (`docs/conventions/feature-layers.md`, `features/library/ports/`, `features/library/adapters/http/`). No new infra, no new dependency, no state or lane event.

Platforms: ios, android, web (no UI change).

Decision: one port per feature (discover, settings). Auth gets adapters and no port: a Supabase wrapper adds a layer and no test value, since its tests already mock `@shared/auth/supabaseClient` in about 12 files.

Decision: one cross-feature port invariants test instead of one copy per feature. Library's own invariants tests stay, since they also pin library's file list and doc.

Waves (a chain, one ticket each, because MC-8's baseline is one exact line): 1) discover port, baseline 28. 2) settings port, baseline 25. 3) auth adapters, baseline 23. 4) cross-feature port invariants plus the doc.

## First slice
A developer writes a discover hook test inside `<DiscoverPortProvider port={createMemoryDiscoverPort({ results })}>` with no `jest.mock`. The app is unchanged: discover search, suggestions and history behave as before on all three platforms.

## Backlog
- Detail on a port (11 MC-8 hits, 49 importers): the biggest feature.
- Playback's api-client calls (10 hits in 8 files): the engines already sit behind the playback contract.
- Shared modules (playlists, favorites, acquisition, offline): the hooks features call.
- `domain/` moves for discover, settings and auth, after the alias codemods #1056–#1058 land. Each move rewrites `fallow-baseline.json`, so the three run in a chain. Move only files that are pure past their direct imports.
- One cross-feature layers invariants test scanning every `features/*/domain/`, with library's regex widened to `react-native-*` and `@react-native*`.
- The impure root files: `reportDiagnostics`, `dangerZoneActions`, `recoveryUnlock`, plus library's 7 from #1136.
- Rename `apps/mobile` to `apps/client`: one solo ticket, since it touches CI, deploy and `~/.claude` lane tooling.
- Rewrite discover and settings tests from `jest.mock` onto the fakes.

## Must-holds
1. Discover and auth make no api-client value import outside `adapters/`, and settings makes none besides the feedback files. [repo-test: apps/mobile/src/features/__tests__/featurePortInvariants.test.ts]
   - Example: `import { suggestDiscovery } from '@shared/api-client/discovery'` in `discover/hooks/useAutocompleteSuggestions.ts` → fails naming the file. `import { submitReport } from '@shared/api-client/feedback'` in `settings/hooks/useSubmitReport.ts` → passes.
2. Each feature's memory fake and HTTP adapter pass the same contract. [repo-test: apps/mobile/src/features/discover/__tests__/discoverPortContract.test.ts]
   - Example: `suggestDiscovery({ q: 'gin' })` on both ports with an aborted signal → both reject with `name === 'AbortError'`.
3. MC-8 equals 23 after the slice. [repo-test: apps/mobile/scripts/__tests__/consistency-ratchet.test.ts]
   - Example: `node scripts/consistency-ratchet.mjs` on main after the slice → prints `MC-8: 23 / baseline 23` and exits 0.

## Expected signals
No endpoint and no page changes. Every API request stays the same. `discover.*` and `settings.*` telemetry stay flat. Sign-in failure reports keep arriving at `/v1/public/auth-failures` at today's rate.

## Decisions
- Default: next slice is discover, settings and auth, the three smallest features (user said "continue").
- Default: auth gets adapters, not a port.
- Default: feedback calls stay put until #376 removes them.
- Default: the `apps/mobile` rename goes to the backlog as a solo ticket.
- Default (after red-team): the `domain/` moves wait for the alias codemods, so this slice is ports only.
