`security` decides how open security findings and failed probes are graded.

- `New` builds the "Security" bucket. `Start` runs one scheduler that probes go-api on an interval, and starting twice still runs one. A non-positive interval falls back to the default.
- A panic in a probe is contained. The probes live in `internal/adapters/probe`, and a go-api that cannot be reached is reported as source-down.
- A restored history ring shows in the snapshot.
