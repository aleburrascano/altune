`sharedtest` owns how integration tests reach a real database.

- `RequireIntegration(t)` skips a test unless `INTEGRATION=1`.
- `DatabaseURL(t)` is the one place a test reads `DATABASE_URL`. With `INTEGRATION=1` and no `DATABASE_URL` the test fails, so an integration run never passes by skipping. A test that needs its own pool settings (one connection, a tracer, a search path) builds them from this URL.
- `Pool(t)` gives a pool on that database, closed when the test ends.
- `TracedPool(t, tracer)` is `Pool` with a `pgx.QueryTracer` on every connection, for a test that counts or gates queries.
- `ExclusiveAcquisitionQueue(t, pool)` holds a Postgres advisory lock until the test ends, so only one test at a time uses acquisition's job queue. A claim takes any pending track, and `go test ./...` runs packages in parallel on one database, so a test that claims, enqueues or counts queue rows takes it first. The acquisition persistence tests and the queue-fence tests in `app/acquisitioncatalog` do. A test that closes its pool itself, or opens a second pool, uses `Pool` instead, because the lock holds one connection until the test ends.
- `Tx(t)` gives a transaction on that database that rolls back when the test ends, for a test that writes raw SQL and must leave no rows behind.
