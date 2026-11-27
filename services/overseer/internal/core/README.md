`core` defines what a bucket, a signal and a series are, and where buckets register.

- `Bucket` is the contract every bucket meets: `Meta`, `Collect`, `Store` and `Snapshot`. Optional hooks add a started loop, rings to persist and a key series.
- `Registry` holds the buckets by id.
- `RingStore` is the bounded in-memory ring of signals a bucket keeps.
- `Series`, `SeriesWriter`, `Point` and `Minute` describe the history a bucket reads and writes. `history` stores them.
- `SLO`, `SLOWindow` and `SLOStatus` count good and total events per day over a 28-day window and report the attainment against a target, and the share of the error budget left. A bucket owns the windows it feeds and puts the statuses in its snapshot data as `slos`. A window lives in memory, so it counts only what overseer saw since it started.
- It imports no other module.
