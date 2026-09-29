# Mobile consistency checklist

Rules that keep business logic consistent across `apps/mobile`, checked mechanically
rather than by LLM judgment. Each rule has an ID, a reason, and a deterministic check.
A rule's `baseline` in `apps/mobile/eslint/consistency/` may only go down: new code must
comply, old code only has to improve. See epic #2857.

| ID | Rule | Why | Check | Baseline | Ticket |
|---|---|---|---|---|---|
| MC-1 | All network calls go through `apiFetch` (`shared/api-client`) | one place to add auth, retries and telemetry to every request | `apps/mobile/eslint/consistency/mc-1-network.js`: no raw `fetch`/`XMLHttpRequest`/`EventSource`/`WebSocket`/`File.downloadFileAsync`/`axios` outside the transport seams; `@shared/auth/supabaseClient` imports baselined at 9 | n/a | #2861 |
| MC-2 | One owning hook per mutating api-client function | prevents divergent retry and error handling for the same mutation | planned: `apps/mobile/eslint/consistency/mc-2-owning-hook.js` | n/a | #2866 |
| MC-3 | Every mutation goes through `useAppMutation` and names its user action | every user action reaches the server as telemetry | `apps/mobile/eslint/consistency/mc-3-app-mutation.js`: no `useMutation` import from `@tanstack/react-query` outside `useAppMutation` and the telemetry sender; action ids in use: `detail.save_track`, `detail.retry`, `library.retry`, `library.delete_track`, `library.delete_tracks`, `library.reacquire`, `settings.clear_search_history`, `discover.clear_search_history`, `playlist.create`, `playlist.create_with_tracks`, `playlist.add_tracks`, `playlist.rename`, `playlist.delete`, `playlist.remove_tracks`, `favorites.toggle`, `settings.submit_report`, `settings.backfill_featured` | 0 | #2862 |
| MC-4 | Every tappable element goes through a shared primitive with a required `action` name | tap telemetry and a11y stay consistent across features | planned: `apps/mobile/eslint/consistency/mc-4-tappable-action.js` | n/a | #2868, #2869 |
| MC-4b | No raw `Pressable` or `Touchable*` imports from `react-native` outside `shared/ui/primitives` and `shared/ui/navigation`; lower `baseline` in the same PR that migrates a file onto a primitive | a raw tappable bypasses primitive tap telemetry, so its taps never reach the server | `apps/mobile/eslint/consistency/mc-4b-raw-pressables.js` | 51 | #2869 |
| MC-5 | Failure UI goes through one failure module that reports `failure_shown` | every failure is visible in telemetry, not just to the user | planned: `apps/mobile/eslint/consistency/mc-5-failure-ui.js` | n/a | #2867 |
| MC-6 | Every server SSE event type has a mobile handler | a new server event can't silently go unhandled on mobile | enforced (bidirectional) by `apps/mobile/src/shared/events/__tests__/eventContract.test.ts`; precheck runs it when go-api event definitions change | n/a | #2859 |
| MC-7 | Server and client agree on telemetry event types | telemetry the server can't parse is telemetry that's lost | enforced (bidirectional) by `apps/mobile/src/shared/telemetry/__tests__/eventContract.test.ts`; precheck runs it when go-api event definitions change | n/a | #2859 |

MC-0 (`apps/mobile/eslint/consistency/mc-0-harness.js`, baseline 0, bans `debugger`
statements) is a self-test of the harness itself, not a checklist rule: it proves the
ratchet catches a violation end to end and can be deleted once a real rule module lands.

## Running the ratchet

```
cd apps/mobile && node scripts/consistency-ratchet.mjs
```

Prints one `MC-n: <count> / baseline <b>` line per rule module and exits 1 if any
count differs from its baseline: fix a new violation, or lower `baseline` in the rule
module once a violation is fixed. `scripts/precheck.sh` runs it on every change under
`apps/mobile/`.
