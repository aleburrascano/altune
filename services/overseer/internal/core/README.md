`core` defines what a bucket, a signal and a series are, and where buckets register.

- `Bucket` is the contract every bucket meets: `Meta`, `Collect`, `Store` and `Snapshot`. Optional hooks add a started loop, rings to persist and a key series.
- `Registry` holds the buckets by id.
- `RingStore` is the bounded in-memory ring of signals a bucket keeps.
- `Series`, `SeriesWriter`, `Point` and `Minute` describe the history a bucket reads and writes. `history` stores them.
- It imports no other module.
