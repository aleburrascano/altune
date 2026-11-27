`core` defines what a bucket, a signal and a series are, and where buckets register.

- `Bucket` is the contract every bucket meets: `Meta`, `Collect`, `Store` and `Snapshot`. Optional hooks add a started loop, rings to persist and a key series.
- `Registry` holds the buckets by id.
- `RingStore` is the bounded in-memory ring of signals a bucket keeps.
- `Series`, `SeriesWriter`, `Point` and `Minute` describe the history a bucket reads and writes. `history` stores them.
- `SLO`, `SLOWindow` and `SLOStatus` count good and total events per day over a 28-day window and report the attainment against a target, and the share of the error budget left. A bucket owns the windows it feeds and puts the statuses in its snapshot data as `slos`. A window lives in memory, so it counts only what overseer saw since it started.
- `SLO.MinTotal` is the fewest events a window needs before it is judged. Below it the status is unmeasured and not met, and the tally is still reported. `SearchAvailabilitySLO`, `SearchLatencySLO` and `SongAcquiredSLO` are the registered objectives, and `slo_register_test.go` fails when their journey, target or minimum differ from the rows of `.claude/slos.tsv`.
- It imports no other module.
