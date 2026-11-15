# Module shape

Every module in the same stack has the same shape, so a reader who knows one knows all of them. The folder tree names what the app does (screaming architecture), and each layer has one job (hexagonal: ports and adapters). go-api is a modular monolith of bounded contexts, and the mobile app is vertical slices with the same layers inside each slice.

Every rule here has an ID and a check. The check fails on a new violation. The violations that exist today are listed in a baseline file, one line per violation. A listed violation that no longer occurs also fails, with "remove it from the baseline", so the list only shrinks and one fix can never pay for a new break. The refactor is done when the tier 1 baselines (discovery, acquisition, playback) are empty.

## go-api and overseer

module-spec owns the Go module shape: each module is a folder with a `module.yaml` under `services/go-api/internal/` or `services/overseer/internal/`. Run `bash scripts/module-spec.sh check`. Its errors link to the rule that explains them.

## mobile: `client/src/features/<feature>/`

| ID | Rule |
|---|---|
| M1 | The feature root holds `domain/`, `ports/`, `adapters/`, `hooks/`, `ui/`, and, where needed, `state/`, `native/`, `web/` and `__tests__/`, plus `index.ts`. No other file or folder sits at the root. |
| M2 | `index.ts` is the feature's public surface. `app/` and other features import `@features/<feature>` only, never a path inside it. |
| M3 | Imports point inward. `domain/` is pure (no `react`, `react-native`, `expo-*` or `@shared/ui` value import). `ports/` holds interfaces, plus the `<Name>PortProvider.tsx` context that injects the port and holds no implementation, and imports `domain/` and apiClient types. `adapters/` implements a port and is the only folder that imports apiClient network calls; the context that supplies the default adapter lives here too. `state/` holds client stores (zustand and in-memory registries) and imports `domain/` and `ports/`. `hooks/` imports `domain/`, `ports/` and `state/`, and `adapters/` may import `state/`; `state/` imports neither. `ui/` imports `hooks/` and `domain/`. |
| M4 | `hooks/` holds only `use*` files, and every `use*` file is in `hooks/`, in a feature or in a layered shared folder. `ui/` holds components; a non-`.tsx` file in `ui/` (styles, labels) is imported only from `ui/`. |
| M5 | Query keys come from the one query-key factory in `@shared/query`. No literal `queryKey: [` elsewhere. |
| M6 | A failure the user sees goes through the shared failure display (MC-5 in [mobile-consistency.md](mobile-consistency.md)). |
| M7 | Test data comes from the factories in `@testing/factories`. |
| M8 | `hooks/`, `ui/` and `adapters/` hold folders, never files. Every non-test file under them sits one folder below: `hooks/<capability>/` and `ui/<capability>/` named for a use case or part of a screen, `adapters/<adapter>/` named for the outside system or technology it talks to (`http`, `platform`, `session`, `files`). In a feature, every `hooks/<x>/` has a `ui/<x>/` of the same name. `domain/`, `ports/`, `state/`, `native/` and `web/` stay flat. A capability or adapter folder name is camelCase and is not an N1 word, a layer name, or any feature's name. |
| M9 | Every folder under `src/features/` and `src/shared/` holds non-test files or folders, never both; `index.ts`, `__tests__/`, `native/` and `web/` are exempt. No folder holds more than 16 non-test source files, and platform variants of one stem (`Foo.tsx`, `Foo.web.tsx`) count as one. |
| M10 | `src/shared/` holds folders only. A `src/shared/<s>/` with a `ports/` folder is layered: it holds only M1 layer folders and `index.ts`, and M3, M4 and M8 apply with every layer optional. Every other `src/shared/<s>/` is flat: files plus `index.ts`, `native/` and `web/`. `src/shared/ui/` holds component-family folders and `index.ts`. |
| M11 | `src/` holds `app/`, `shell/`, `wiring/`, `features/` and `shared/`. `app/` holds expo-router route files, each importing its screen from a feature; auth screens reached from an email link live under `app/auth/` (the Supabase `uri_allow_list` is `/auth/**`), and auth screens reached inside the app under `app/(signedOut)/`. `shell/` holds the frame around every screen (sidebar, player bar, staging banner, per-platform extras). `wiring/` gives each feature its adapters at startup, as go-api's `internal/app/` does. |

## Names, both stacks

| ID | Rule |
|---|---|
| N1 | No file or folder is named `util`, `utils`, `helper`, `helpers`, `common`, `misc`, `lib`, `core`, `manager`, `base`, `generic`, `data`, `stuff`, `tmp`, `temp`, `new`, `old`, `types` or `constants`. Name it for the one thing it holds. `index.ts` as a barrel is allowed. |
| N2 | A module or feature folder is named for a domain concept in [ubiquitous-language.md](../ubiquitous-language.md), and the same concept has the same name in every stack. |
| N3 | Mobile only: a file is named for its concept and may hold several related exports that change together (Common Closure Principle), so do not split a file one export at a time. File and folder names under `src/features/` and `src/shared/` are camelCase; a `.tsx` file whose default or main export is a React component is PascalCase. Platform suffixes (`.web`, `.native`, `.android`, `.ios`) and `index.ts` are allowed. `src/app/` is exempt, since expo-router route names are URLs. Go files already hold several related declarations under Go's own casing, so go-api has no N3; N1 and N2 still apply there. |
| N4 | Both stacks: a file, folder or exported name, split into words, contains none of `manager`, `helper`, `util`, `common`, `misc`, `stuff`, `data`, `info`, `process`, `processor`, `logic`, `parts`, `extras`, `wrapper`, `thing`, a `v1`/`v2` suffix or a lone `A`/`B` suffix. `handler` is allowed only for go-api HTTP handlers and `slog.Handler`, and TS `handle*` locals and `on*` props. A glossary term wins over the list. This extends N1 from whole names to word parts. |
| N5 | Both stacks: every module, feature, capability and adapter folder name, split into words, is a term in [ubiquitous-language.md](../ubiquitous-language.md) or in its "Structural words" list (`screen`, `http`, `platform`, `wire`, ...). The glossary's "Banned terms" section is read by both checkers. |

## Client folders and names (M8 to M11, N4, N5)

The client takes go-api's G8 approach: one folder scheme in every feature, however small, a 16-file cap, and names a newcomer reads correctly. A name that is not plainly clear to a human is a tell that something should change.

Change scenarios, counted as non-test files in the folders you open to find your way, then folders opened:

| Change | Before | After |
|---|---|---|
| Add a detail-screen action ("Share track") | 47 / 2 | 19 / 3 |
| Fix a playback bug (scrubber snaps back after a seek) | 36 / 3 | 31 / 3, since `native/` is unchanged |
| Remove the kill-switched detail enrichment UI | 60 / 4 | 37 / 7 |
| Add a settings toggle | 17 / 3 | 9 / 4 |
| Add a shared UI primitive (`Switch`) | 39 / 3 | 16 / 3 |
| Change search history behaviour | 54 / 5 | 9 / 5 |
| Remove the Featuring screen | 72 / 4 | 4 / 4 |

Every change reads fewer files. A change that crosses capabilities opens more folders.

Where the client differs from G8:

- **`src/app/` is exempt from M8 to M10**, since expo-router derives URLs from file paths. M11 still fixes where each kind of route lives.
- **`native/` and `web/` stay as platform folders.** They are the ESLint platform-API boundary, so they are not folded into `adapters/`.
- **The cap counts stems**, so `useOAuth.ts` and `useOAuth.web.ts` are one concept.
- **Folder names are camelCase**, following N3.
- **`hooks/` and `ui/` share capability names**, so one use case is one word in both places.
- **`state/` is a client-only layer.** Stores import `zustand` or `react`, which M3 bars from `domain/`.
- **Tests stay in `__tests__/`.** A move changes only their import paths.

Decisions:

- **Small features split too.** Settings becomes several 1 to 2 file capability folders. One shape beats fewer folders.
- **The 16-file cap applies to `domain/` as well.** Only `playback/domain` is over (18); moving its three platform files out brings it to 15.
- **`shared/acquisition`, `shared/offline` and `shared/playlists` stay in `shared/`,** in the layered shape. Features may not import each other, and four features use them.
- **Library is the canonical term in both stacks.** go-api's `catalog` module is renamed `library` (N2), after acquisition's work lands, since acquisition imports catalog in 27 files, and after the G8 moves.
- **`chrome/` splits into `shell/` and `wiring/`.** "Chrome" is browser jargon a newcomer misreads, and the folder held two jobs: the frame around screens and the startup wiring of ports to adapters.
- **Auth routes have two homes.** `reset-password` moves under `app/auth/`, which stays inside the allow list, and the `(auth)` group becomes `(signedOut)`, so it no longer looks like the `auth/` folder.
- **Move first, rename after,** one feature per PR, then one concept per rename PR. Glossary fixes land before N5's check.

## Where the checks live

- go-api: module-spec, with its baseline in `services/go-api/module-spec.baseline.json`.
- mobile: one jest test walks `src/features/` and `src/shared/` for M1, M2, M4 and N1 to N3. ESLint zones carry M3 and M5. M8 to M11, N4 and N5 land in #2306. Baseline: `client/baselines/module-shape.baseline`.
- Both run in `scripts/precheck.sh`, so a PR that adds drift fails before review.
