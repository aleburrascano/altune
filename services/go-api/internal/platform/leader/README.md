`leader` owns which backend instance is the leader.

An election runs over a `LockStore` and ticks at the interval its caller gives. `NewElection` takes the store, the lock key, the interval, the clock's `now`, its `newTimer` and its `newTicker`, and production passes the Postgres advisory lock that `shared/database` provides, adapted with `AdaptLocks`, so this module names no database driver.
