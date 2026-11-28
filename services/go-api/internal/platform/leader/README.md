`leader` owns which backend instance is the leader.

An election runs over a `LockStore` and ticks at the interval its caller gives. `NewElection` takes the store, the lock key, the interval, the clock's `now`, its `newTimer` and its `newTicker`, and production passes the Postgres advisory lock that `shared/database` provides, adapted with `AdaptLocks`, so this module names no database driver.

`LockStore.Acquire` returns a `ports.LockSession`, which a store outside this module satisfies structurally through `AdaptLocks`.

## Must not

- Name a database driver or read config: the store and every timing come in through `NewElection`.
- Serve a route or own a table.

## Shape

The door is `leader.go`. Under `internal/`: `domain/` (`errors.go` with `ErrLeadershipLost`, `counters.go` with the `Counters` snapshot that `app/livemetrics` serializes), `ports/ports.go` (`LockStore`, `LockSession`) and `service/` (the election loop, its term and its counters). `leadertest/` holds `ErrLeadershipLost` for the job runner's tests in `app`.
