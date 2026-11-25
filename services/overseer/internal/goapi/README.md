`goapi` is how the overseer reads go-api's observe endpoints and classifies their failures.

- The readers for health, metrics, eval, acquisition, quality, events, logs and provider usage live in `internal/adapters/goapihttp`.
- `SharedTokenSource` builds the credential every reader sends, and `Classify` and `IsSourceDown` turn a failure into the reason a bucket shows.
- `NewNullEventSource` stands in for a stream that is down, and `StreamStatus`, `DrainPending` and `TotalDropped` report on a stream.
- The contract tests decode fixtures in `testdata/contract`. The health, events and logs frames are symlinks to go-api's own fixtures, so drift there fails here. The other fixtures are local copies.
