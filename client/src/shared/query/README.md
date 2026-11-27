`query` owns how server data is cached, keyed and retried.

Every query says when its data goes stale with a policy from `STALE`: `STALE.untilEvent` (kept until a server event or an explicit invalidation refreshes it), `STALE.minute`, `STALE.halfHour`, `STALE.day`, `STALE.now` and `STALE.default` (the client default). `staleTime` never takes a bare number or `Infinity`; `client/eslint.config.js` fails both.

A server event is the one home for what changed: `EVENT_INVALIDATES` in `@shared/events` maps each event type to the query keys it refreshes. A mutation only patches the cache optimistically and rolls the patch back on failure; it never calls `invalidateQueries` (the lint rule fails it outside `shared/events` and `shared/query/shell`). Where no server event exists for the change, the mutation names the keys with `refetchWithoutEvent(queryClient, keys)`, or passes them as `invalidate` to `useOptimisticMutation`.

`useOptimisticMutation` is the home of an optimistic write to one query key: it pins the session, snapshots the cache, writes, rolls back with an alert and keeps the retry options. Six callers stay hand-written because their write is not one query key:

- `useClearSearchHistory` empties the list even when none is loaded yet, and refetches only when the clear fails, which a snapshot-and-revert on a loaded key cannot do.
- `useFavorites` coalesces rapid taps per favourite in a lane, so one request runs at a time and the last tap wins; a per-call snapshot would roll an earlier tap back over a later one.
- `useDeleteTracks` and `useSaveTrack` write to every track cache family and the track status store (delete also the pinned store, save also the download memory and the identity links), and undo them together.
- `useRetryAcquisition` patches one track across every track cache family and `useRetryTrack` patches the track status store; both run on the shared retry mutation of `@shared/acquisition` and restore the previous acquisition state, so neither has a query key to snapshot.
