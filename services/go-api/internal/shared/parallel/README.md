`fanout` owns how many concurrent calls one fan-out may run.

Each task runs through `runloop.Try`, so a panicking task is logged once and comes back from `Wait` as a `*runloop.PanicError` while the other tasks finish.
