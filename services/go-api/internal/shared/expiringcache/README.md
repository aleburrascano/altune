`expiringcache` owns the one in-process memo: a keyed, bounded store whose entries expire after a fixed time.

A full memo makes room by dropping expired entries first (swept at most once per TTL), then the least recently used. `Put` on a known key replaces the value and restarts its TTL. A cap below one holds one entry. The clock is injected, so tests move time instead of waiting.

A memo is not a cache of record: it has no single-flight, no stats and no invalidation. Callers that need those wrap it.

`Keyed` is the one bounded registry of per-key rate limiters (`*rate.Limiter` or anything with `TokensAt` and `Burst`). `Get(key, create)` builds a limiter on first use. It has one eviction rule: a key untouched for two idle periods is dropped (two generations, so the sweep costs nothing per call), and at the key cap it drops fully refilled limiters first, then the least penalised ones (most tokens left) down to 90% of the cap, so a client that is being throttled is the last to be forgotten. Pick the idle period no shorter than the time a full bucket takes to refill. Used by the per-user HTTP limiter, the auth failure throttle and outbound host pacing.
