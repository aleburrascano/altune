`settings` owns what the user can change about the app and their account.

The door, `index.ts`, exports only what `mobile` uses: `SettingsScreen`, `SettingsProvider`, `httpSettingsPort` and the `SettingsPort` type. Tests reach the parts directly.

- `core/`: `accountEmail`, `backfillResult`, `backfillStatus`, `dangerZoneActions`, `downloadStatsModel`, `removeDownloads`, `retryOptions`. Settings has no failure type of its own (the one failure it raises is `ContractError` from `@shared/failure`) and no store.
- `ports/ports.ts`: `SettingsPort`, the one port, filled by `mobile`.
- `adapters/http/`: `settingsHttp.ts` fills the port, and `mapping.ts` maps the `@shared/apiClient` result to the `core` type. No other part imports `@shared/apiClient`.
- `view/`: `SettingsProvider.tsx` hands the port to the hooks, the screen and its cards, and the `use<Thing>.ts` hooks.
- `__tests__/`: every test.
