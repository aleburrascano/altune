`sharedtest` owns how integration tests reach a real database.

- `RequireIntegration(t)` skips a test unless `INTEGRATION=1`.
- `DatabaseURL(t)` is the one place a test reads `DATABASE_URL`. With `INTEGRATION=1` and no `DATABASE_URL` the test fails, so an integration run never passes by skipping. A test that needs its own pool settings (one connection, a tracer, a search path) builds them from this URL.
- `Pool(t)` gives a pool on that database, closed when the test ends.
- `Tx(t)` gives a transaction on that database that rolls back when the test ends, for a test that writes raw SQL and must leave no rows behind.
