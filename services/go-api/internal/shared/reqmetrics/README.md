`reqmetrics` owns how request latency is measured and reported: the composition root creates one `Registry`, `Middleware` records each request into it, and its `Snapshot` reports it.
