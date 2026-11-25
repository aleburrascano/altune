`history` decides how bucket samples persist across restarts.

- `Open` returns a `Database` that stores points, rollups and signals in SQLite. A missing file starts empty, and a corrupt file or a missing directory starts unavailable instead of failing.
- `WithSchemaTimeout` bounds how long the schema may take before history falls back to unavailable.
- An unavailable history restores nothing and accepts writes.
- `app` restores and persists the buckets' rings through it. The SQLite code lives in `internal/adapters/sqlite`, and the module owns the `points`, `rollups` and `signals` tables.
