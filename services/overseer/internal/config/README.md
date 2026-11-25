`config` decides which settings the overseer runs with and when they are valid.

- `Load` reads every setting through the environment reader it is given, applies defaults and returns an error when a required one is missing or malformed.
- The owner user id, the Supabase URL and the anon key are required. The port, tick, bucket timeout, base path and cost interval are checked.
- The configuration redacts its secrets when logged.
- It is the only module that decides what an environment variable means, and only `cmd/overseer` passes it `os.Getenv`.
