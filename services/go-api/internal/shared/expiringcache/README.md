`expiringcache` owns the one in-process memo: a keyed, bounded store whose entries expire after a fixed time.

A full memo makes room by dropping expired entries first (swept at most once per TTL), then the least recently used. `Put` on a known key replaces the value and restarts its TTL. A cap below one holds one entry. The clock is injected, so tests move time instead of waiting.

A memo is not a cache of record: it has no single-flight and no invalidation. Callers that need those wrap it.

`Stats` reports how many `Get` calls were served (hits) and not served (misses) since the memo was made; an expired entry counts as a miss. Callers log it next to their own work so a memo's worth shows as a hit rate. `Peek` reads like `Get` but is not counted, for a second look inside a single-flight.
