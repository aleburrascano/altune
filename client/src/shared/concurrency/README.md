`concurrency` owns how many async jobs run at once and how typing is debounced.

`runBounded` runs a batch with a cap. `useDebouncedValue(value, ms)` settles a value after it stops changing. `useDebouncedCall(ms)` returns `schedule(run)` and `cancel()` for a delayed call that restarts on each schedule and never runs after unmount. A hook that needs a delay uses one of these; it does not call `setTimeout` itself.
