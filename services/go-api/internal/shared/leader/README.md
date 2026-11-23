`leader` owns which backend instance is the leader.

An election runs over a `LockStore` and ticks at the interval its caller gives. `NewElection` takes the store, the lock key and the interval, and production passes `PoolLocks`, the Postgres advisory lock.
