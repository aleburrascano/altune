`events` owns how server-sent events reach the app and patch the cached data.

A server event is the one home for what changed in the cache. `EVENT_INVALIDATES` (`core/eventInvalidation.ts`) maps every event type to the query keys it refreshes, an empty list where the handler patches the cache instead, and the handlers read it through `invalidateForEvent`. Only `shared/events` and `shared/query/shell` call `invalidateQueries`; a mutation patches optimistically, rolls back, and names keys itself only where no event exists, with `refetchWithoutEvent` from `@shared/query`.
