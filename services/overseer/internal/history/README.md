`history` decides how bucket samples persist across restarts.

- `Open` returns a `Database` that stores points, rollups and signals in SQLite, and starts empty and unavailable instead of failing when the file is missing or corrupt.
- `WithSchemaTimeout` bounds how long the schema may take before history falls back to unavailable.
- An unavailable history restores nothing and accepts writes.
- The SQLite code lives in `internal/adapters/sqlite`, and the module owns the `points`, `rollups` and `signals` tables.
