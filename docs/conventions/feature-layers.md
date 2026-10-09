# Feature layers

A feature that talks to the API is split into layers so its logic runs without the network,
React or the platform. Library (`apps/mobile/src/features/library`) is the worked example.

## Which features are ported

- Ported (port, HTTP adapter and memory fake): library, discover and settings.
- Adapters with no port: auth. Its `adapters/http/` files hold the raw fetches and also import
  Supabase, `@shared/device/device` and the kill switch, an exception to the table's "may import"
  column.
- Not yet ported: detail and playback.

## Layout

| Folder           | Holds                                                                                                                                                                                                                           | May import                                                                                                                                                                |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `domain/`        | Pure rules: sort, state, refresh, groupPaging, failureLogFields, goBackOrToLibrary, featuringDetailRoute, library-to-discovery, activeView, pagedListControls, libraryFailureAlert, trackNotFoundAlert, logTrackMutationFailure | Types and other pure modules only. No value import from `react`, `react-native`, `expo-*`, `lucide-react-native`, the `@shared/ui` barrel, `hooks/`, `ui/` or `adapters/` |
| `ports/`         | The interfaces the feature owns, and the context that supplies one                                                                                                                                                              | `domain/`, type imports of api-client                                                                                                                                     |
| `adapters/http/` | The only place the feature imports api-client network functions                                                                                                                                                                 | `ports/`, `@shared/api-client`                                                                                                                                            |
| `hooks/`         | React glue: query hooks and actions that read the port from context                                                                                                                                                             | `domain/`, `ports/`                                                                                                                                                       |
| `ui/`            | Screens and components                                                                                                                                                                                                          | `hooks/`, `domain/`                                                                                                                                                       |

## Moving the next feature

1. Move the pure files into `domain/` (`git mv`, then fix import specifiers).
2. Write the port as an interface whose members are `typeof` the api-client functions,
   as in `ports/libraryPort.ts`.
3. Write the HTTP adapter as an object literal that forwards each call, as in
   `adapters/http/httpLibraryPort.ts`.
4. Put a context in `ports/LibraryPortProvider.tsx` with the HTTP adapter as its default, so
   production needs no provider and tests pass a fake through `LibraryPortProvider`.
5. Write a memory fake, `__tests__/memoryLibraryPort.ts`.
6. Write a `describe.each` contract test, `__tests__/libraryPortContract.test.ts`, that runs the
   same cases against the HTTP adapter (fake fetch) and the memory fake, so the fake cannot drift.
7. Point hooks at the port (`useLibraryPort()`) and drop their api-client imports.

## How it is enforced

- MC-8 (`apps/mobile/eslint/consistency/mc-8-feature-ports.js`, see
  [mobile-consistency.md](mobile-consistency.md)) bans value imports from `@shared/api-client`
  outside `adapters/`.
- `__tests__/libraryPortInvariants.test.ts` pins library's port boundary.
- `__tests__/libraryLayersInvariants.test.ts` pins library's domain files and this doc.
- `features/__tests__/featurePortInvariants.test.ts` checks library, discover and auth make no
  api-client value import outside `adapters/`, and neither does settings, detail or playback.
- `features/__tests__/featureLayersInvariants.test.ts` runs the domain purity check over every
  `features/*/domain/` folder. Every feature uses the domain/ports/adapters/hooks/ui layout, and
  the same test ratchets the loose `.ts`/`.tsx` files at each feature root: a count over its
  ceiling fails, and a count under it fails with "lower the ceiling", so moves only ever lower it.

## The purity rule

The domain check reads direct imports only. A file whose imports pull in the platform one step
down, or that registers a listener when it is loaded, stays out of `domain/`.
