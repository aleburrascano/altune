`usage` decides how much the app is used over time.

- `New` builds the "Usage" bucket. `Start` runs the pump that reads go-api's event stream, and `Collect` turns each pending event into a signal stamped no later than now.
- It keeps a bounded timeline and a bounded search rollup that counts masked searches and ranks them by count, then by query. The frequent query survives eviction.
