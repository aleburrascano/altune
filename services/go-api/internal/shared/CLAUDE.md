# internal/shared — router

Cross-cutting infrastructure. The one layer domain code may import besides stdlib — and from it, only shared value objects like `UserId` plus the pure `textnorm` functions. Enforced by depguard (`.golangci.yml`, CI), which also blocks anything here from importing a feature package.

Layout:

- `config/` — `Config` and `Load`; every env flag lives here.
- `database/` / `redis/` — connection pools.
- `logging/` — `Setup`, the `ringHandler` and the `RingBuffer` behind Mission Control's logs panel.
- `events/` — `InProcessBus`, the SSE event bus.
- `httputil/` — `StatusError`, `HandleServiceError`, correlation/logging/recovery middleware.
- `httptrace/` — `Recorder` and `Replayer`.
- `textnorm/` — `NormalizeForMatch`, `TokenSortRatio`, `LevenshteinDistance`.
- `phonetics/` — `DoubleMetaphone`, `MetaphoneKey`.

## Rules

- Never simplify `stripSymbols` back to a `[^\w\s]` regex — it must stay Unicode-aware.
- Never add a hand-curated word list to `NormalizeForMatch`.
- Never assume a non-nil Redis client works: `NewClient` returns non-nil even when the ping fails, so cache adapters must tolerate failing calls at runtime.
- Never import `net/http` from a `domain/` package — carry the status on a structural `StatusError` instead.
- Never add a response-writer wrapper that doesn't forward `Flush` — SSE breaks silently.
- Never retain a `slog.Record` past `Handle`; copy what you need first.
- Never block the logging or publish hot path on a slow subscriber — drop instead.
- Never inject `Recorder` into a production client.
- Never answer an unmatched request in `Replayer` with an empty response — it must be a hard error.
- Never rename a metaphone rule casually: every stored vocabulary key built from it goes stale.
- Never log a config value — `LogValue` reports `has_*` booleans only.

Why each rule exists, and what every config flag does: `okf/backend/shared-infra.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
