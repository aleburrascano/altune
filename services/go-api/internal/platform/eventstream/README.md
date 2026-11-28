`eventstream` owns how bus events reach each user's live event stream, with replay.

## Must not

- Publish events or decide what an event means: it only forwards what `shared/events` holds.
- Read config: `app` passes the connection cap and the shutdown channel in `Deps`; the heartbeat and write timeout default unless a test sets them.
- Own a table.

## Shape

The door is `eventstream.go`: `New(Deps)` returns the `Routes` that `app` mounts in its long-lived group (`/events`). Under `internal/`: `domain/` (`errors.go` and the connection limiter), `ports/ports.go` (`EventSubscriber`) and `adapters/handler/` (`routes.go`, the stream handler, `sse_conn.go` and `errors.go`, which maps a connection-limit error to its log event). The stream writes `events.Event` frames as they are, so there is no `dto.go`.
