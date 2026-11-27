`query` owns how server data is cached, keyed and retried.

Every query says when its data goes stale with a policy from `STALE`: `STALE.untilEvent` (kept until a server event or an explicit invalidation refreshes it), `STALE.minute`, `STALE.now` and `STALE.default` (the client default). `staleTime` never takes a bare number or `Infinity`; `client/eslint.config.js` fails both.
