# Module shape

Every module in the same stack has the same shape, so a reader who knows one knows all of them. The folder tree names what the app does (screaming architecture), and each layer has one job (hexagonal: ports and adapters). go-api is a modular monolith of bounded contexts, and the mobile app is vertical slices with the same layers inside each slice.

Every rule here has an ID and a check. The check fails on a new violation. The violations that exist today are listed in a baseline file, one line per violation. A listed violation that no longer occurs also fails, with "remove it from the baseline", so the list only shrinks and one fix can never pay for a new break. The refactor is done when the tier 1 baselines (discovery, acquisition, playback) are empty.

## go-api: `services/go-api/internal/<module>/`

A module is every folder under `internal/` except `app/` (the composition root, wiring only) and `shared/` (the kernel every module may import).

| ID | Rule |
|---|---|
| G1 | The module root holds `domain/`, `ports/`, `service/`, `adapters/` and `ARCHITECTURE.md`, and nothing else. No `.go` file sits at the root. |
| G2 | Imports point inward. `domain/` imports the standard library and `shared/` only. `ports/` adds `domain/`. `service/` adds `ports/`. Only `adapters/` imports drivers, HTTP, pgx or another module. The rule is derived from the folder name, so a new module is covered with no config edit. |
| G3 | A module reaches another module only from `adapters/<other>bridge/`, and that package imports the other module's `ports/` or `service/`. A package named `*bridge` that imports nothing from its module is misnamed. |
| G4 | An error a caller branches on is declared in `domain/errors.go`. A handler writes every error through `httputil.HandleServiceError`. `http.Error` and a bare error `WriteHeader` appear only in `shared/httputil/`. |
| G5 | One handler per resource in `adapters/handler/`, named `<resource>_handler.go`, built by `New<Resource>Handler`. |
| G6 | Fakes used by more than one test file live in the module's test package, `<module>/<module>test/`, as `catalog/catalogtest/` does. |
| G7 | Logs use the `slog.*Context` calls only. |

## mobile: `apps/mobile/src/features/<feature>/`

| ID | Rule |
|---|---|
| M1 | The feature root holds `domain/`, `ports/`, `adapters/`, `hooks/`, `ui/`, `__tests__/`, `native/` and `web/` where needed, and `index.ts`. No other file sits at the root. |
| M2 | `index.ts` is the feature's public surface. `app/` and other features import `@features/<feature>` only, never a path inside it. |
| M3 | Imports point inward. `domain/` is pure (no `react`, `react-native`, `expo-*` or `@shared/ui` value import). `ports/` holds interfaces only and imports `domain/` and api-client types. `adapters/` implements a port and is the only folder that imports api-client network calls; the context that supplies the default adapter lives here too. `hooks/` imports `domain/` and `ports/`. `ui/` imports `hooks/` and `domain/`. |
| M4 | `hooks/` holds only `use*` files, and every `use*` file is in `hooks/`. `ui/` holds components only. |
| M5 | Query keys come from the one query-key factory in `@shared/query`. No literal `queryKey: [` elsewhere. |
| M6 | A failure the user sees goes through the shared failure display (MC-5 in [mobile-consistency.md](mobile-consistency.md)). |
| M7 | Test data comes from the factories in `@testing/factories`. |

## Names, both stacks

| ID | Rule |
|---|---|
| N1 | No file or folder is named `util`, `utils`, `helper`, `helpers`, `common`, `misc`, `lib`, `core`, `manager`, `base`, `generic`, `data`, `stuff`, `tmp`, `temp`, `new`, `old`, `types` or `constants`. Name it for the one thing it holds. `index.ts` as a barrel is allowed. |
| N2 | A module or feature folder is named for a domain concept in [ubiquitous-language.md](../ubiquitous-language.md), and the same concept has the same name in every stack. |
| N3 | A file holds one concept and is named for it: the main type or function it exports, in the stack's casing. |

## Where the checks live

- go-api: one Go test walks `internal/` with `go/parser` for G1 to G7 and N1 to N3. Baseline: `services/go-api/module-shape.baseline`.
- mobile: one jest test walks `src/features/` and `src/shared/` for M1, M2, M4 and N1 to N3. ESLint zones carry M3 and M5. Baseline: `apps/mobile/module-shape.baseline`.
- Both run in `scripts/precheck.sh`, so a PR that adds drift fails before review.
