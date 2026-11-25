`config` decides which settings the overseer runs with and when they are valid.

- `Load` reads the server settings through the environment reader it is given, applies defaults and returns an error when a required one is missing or malformed.
- The owner user id, the Supabase URL and the anon key are required. The port, tick, bucket timeout and cost interval are checked, and the base path is normalised.
- The configuration redacts its secrets when logged.
- The bucket and credential variables are read by `app` and `goapi` from the same reader, and only `cmd/overseer` passes it `os.Getenv`.
