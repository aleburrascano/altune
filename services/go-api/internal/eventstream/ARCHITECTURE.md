# Eventstream — Module Architecture

The eventstream bounded context (`services/go-api/internal/eventstream/`) serves the per-user SSE **Event stream** of bus events, with `Last-Event-ID` replay.

- `domain/` — the connection-limit sentinels `ErrUserConnLimit` and `ErrGlobalConnLimit`.
- `ports/` — what the stream needs from outside: `EventSubscriber` (`Subscribe`, `LatestID`, `HighestIssuedID`, `Replay`, `ResumeGapped`).
- `service/` — `ConnLimiter`, the per-user and global cap on open streams.
- `adapters/` — `handler` is the HTTP/SSE edge (`EventStreamHandler`, `sseConn`); `authbridge` reads the request user id and the token expiry from auth.
