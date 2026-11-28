Infrastructure: `parallel` owns how many concurrent calls one fan-out may run.

Each task runs through `background.SafeRun`, so a panicking task is logged once and comes back from `Wait` as a `*background.PanicError` while the other tasks finish.

`Slots` is the one acquire-or-shed pool: `Acquire(ctx)` waits for a free slot, `TryAcquire()` refuses at once when all are held, and `Release()` frees one. The download concurrency limit, the soundcloud yt-dlp slots and the filesystem call gate use it.
