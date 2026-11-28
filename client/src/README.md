Each folder under `features/` and `shared/` is one module, and its folder says its role. The first line of a module's `README.md` says what it owns.

- `features/<name>/` is a feature. It imports only shared modules, never another feature, so each one can change alone. When two features must work together, `mobile` connects them through a port the feature owns.
- `shared/<name>/` is shared. Any module may import it, and it imports only other shared modules.
- `mobile/` is the app. It builds every module, fills each port with its adapter, and draws the frame around every screen. Only it reads `process.env`, and it passes the values down.
- `app/` holds the expo-router routes, because expo-router builds its URLs from this folder. `app` and `mobile` may import any module.

Code reaches another module only through its door, `index.ts`. Inside a feature or shared module, every file but the door sits in one of five parts:

- `core/`: the rules and their types, as plain functions. It imports only `core/` and `ports/`, and no package but `@tanstack/react-query` and `zustand`.
- `ports/`: the interfaces `core/` or `shell/` need, which adapters fill. It imports only `core/` and `ports/`, and the same two packages.
- `shell/`: connects the core to the ports and adapters. It imports `core/`, `ports/`, `adapters/` and `shell/`, and the same two packages.
- `view/`: the screens, components and the hooks they use. It is the only part that imports the UI framework (`react`, `react-native`, the expo UI packages, `lucide-react-native`) or draws JSX. It imports `core/`, `ports/` and `view/`.
- `adapters/`: the only part that does I/O: `fetch`, storage, native modules and every package that is neither pure nor UI. It imports `core/`, `ports/` and `adapters/`, and never the UI framework.

Code for one platform is a platform file (`x.native.ts`, `x.web.ts`) beside the shared one in its part. Metro loads each platform file as a root and loads the shared file on every platform that has no file of its own, but fallow and react-doctor resolve an import to one file only. `client/knip.json` is the one list both read: a glob for the `.android`, `.ios` and `.native` files, and one line for each shared file that has platform files beside it. `.web` files are not on the list, because both tools already resolve an import to them, so an unused export in a `.web` file is still reported. When fallow or react-doctor reports an unused export in a shared file that has a platform file beside it, add that file to `client/knip.json`.

Tests sit in a `__tests__/` folder. A test reaches the parts of its own module directly, other modules only through the doors its module may import, and helpers through `@testing/`. It hands the module its fakes through the module's `create*(deps)` door and never patches what the module imports: no `jest.mock`, `jest.doMock`, `jest.spyOn`, `jest.unstable_mockModule`, or their `vi.` forms. A test never waits on the real clock, so `setTimeout`, `setInterval`, `setImmediate` and `node:timers` are lint errors in it: it flushes pending promises with `flushPromises` from `@testing/flushPromises` (the one place that ticks the real loop; it works under fake timers because they do not fake `node:timers/promises`), waits for the condition with `waitFor`, or runs `jest.useFakeTimers()` and advances time; a slow fake dependency under fake timers uses `resolveAfter` or `rejectAfter` from `@testing/delays`, which are for fake-timer tests only. Each mutant Stryker makes on a changed line must fail one of the module's own tests.

Work that must finish by a deadline goes through `withDeadline(work, ms, { onExpire })` from `@shared/timing`, never `Promise.race`. It owns the timer, clears it when the work settles, and hands a failure that comes after the deadline to `onLate`, so it is not lost.

What a failure means and what the user reads about it live in `@shared/failure`: `failureKindOf(error)` names the kind and `describeError(error, action?)` words it. Code never compares an HTTP status to a bare number (`status >= 500`, `500 <= status`, `case 404:`, a `*_STATUSES` set): it asks for the kind, or uses a named constant from `@shared/failure` (`HTTP_*`). `client/eslint.config.js` fails the bare forms. `apiErrorCodes.generated.ts` is written by `cd services/go-api && go run ./scripts/genclientcodes ../../client/src/shared/failure/core/apiErrorCodes.generated.ts` from the server's catalog; never edit it by hand.

| Rule                                                                                          | Checked by                                         |
| --------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| Module imports and doors                                                                      | fallow, `client/fallow/modules.json`               |
| Part imports, every file in a part, no `fetch` outside adapters                               | fallow, `client/fallow/parts.json`                 |
| No duplicated code                                                                            | `fallow dupes`                                     |
| Packages and JSX per part, tests through doors, no patching, `process.env`, no `Promise.race` | eslint, `client/eslint.config.js`                  |
| Each changed line is tested                                                                   | Stryker, `bash scripts/ci/client-mutate.sh <base>` |

Call a collaborator, not its parts: keep `deps.foreground` in a field and call `this.foreground.isActive()`, never `this.deps.foreground.isActive()`. Reading plain data (`props.theme.color`) is exempt.

For now the fallow and eslint rules above check only the files a branch changes, because older code still breaks them: `node client/scripts/fallow-changed.mjs <base>` and eslint on the changed files. Until then `npm run lint` on the whole tree fails on the older code. `bash scripts/precheck.sh` runs every check.
