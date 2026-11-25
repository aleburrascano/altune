`oci` is how the overseer reads the cloud tenancy's spend.

- It returns a `Spend` with its lines, and a source-down error that names the usage API and keeps its cause.
- The usage API client lives in `internal/adapters/usage` and the types in `internal/core`.
