# internal/shared — router

Cross-cutting infrastructure: config, DB/Redis pools, structured logging + ring buffer, HTTP trace record/replay, httputil, textnorm, `UserId`. Per `okf/backend/index.md`, this is the one layer domain code may import besides stdlib — and from it, only shared value objects like `UserId` plus the pure `textnorm` functions. Enforced by depguard (`services/go-api/.golangci.yml`, CI), which also blocks anything here from importing a feature package.

Gotchas:

- Redis: `NewClient` returns a **non-nil** client even when the ping fails — cache adapters must tolerate failing Redis calls at runtime, not just check for nil.
- `textnorm.NormalizeForMatch` is Unicode-aware by design — **don't "simplify" `stripSymbols` back to a `[^\w\s]` regex.** Go's `\w` is ASCII-only, so that regex deleted every CJK / non-Latin letter: "坂本龍一" normalized to `""` and became unrankable (empty qnorm disables relevance). `isWordContent` keeps letters and numbers of any script while still dropping symbols and hyphens; for ASCII input the output is byte-identical to the old regex. Matching is symmetric (query and title both go through it), so keeping more letters cannot break a match. Keeping symbols (the artist `¥$`, the `07-The Best …` hyphen-tokenization trap) stays an eval-matcher concern — fix it in the matcher, not here.
- `NormalizeForMatch` deliberately has **no** hand-curated word lists. A leading-article strip (the/los/les/el/la/le) and feature-token normalization (feat/ft/featuring/with) were removed in 2026-06 — they were fit to expected catalog languages, and the "with" rule mangled real titles like "Stuck with U". Both sides of a comparison keep their articles, so matching is unaffected.
- `stripDiacritics` recomposes to NFC after dropping combining marks: a no-op for Latin bases, but it recomposes decomposed scripts like Hangul from jamo back to syllables, so the output is canonical rather than jamo-decomposed.
- Domain errors reach HTTP via the structural `StatusError` interface and `HandleServiceError` — `domain/` never imports `net/http`.
- The log ring buffer captures at Debug (`ringCaptureFloor`) regardless of stdout level; it feeds Mission Control's logs panel, so the operator keeps rich provider/breaker DEBUG lines even when production stdout runs at INFO. `Handle` forwards to stdout only when stdout's own level wants the record, so those DEBUG lines aren't also printed.
- `CapturedRecord` is a flattened *copy*. The originating `slog.Record` is never retained — its `Attrs` are unsafe to read after `Handle` returns. `flattenedAttrs` builds the copy before `append` takes the ring's mutex, so the inner handler's formatting lock is never held across the ring's.
- The ring's live-tail fan-out is lossy by design (like the SSE event bus): `fanOutDroppingWhenSubscriberIsFull` drops for a slow subscriber rather than blocking the logging hot path. `logRingCapacity` is sized for a single-operator console on a memory-constrained box, not durable retention.

## Event bus (`events/bus.go`)

- `InProcessBus.users` grows one entry per distinct `UserId` and is **never evicted** — a few hundred bytes per user, bounded in practice by the family-scale user base, and reset on restart like all in-memory state.
- `idBaseMonotonicAcrossRestarts` seeds every user's counter from the wall clock at process start. Without it the per-user `nextID` restarts near 0 (regression F5), and a client that had already seen low ids from the previous process would mis-dedupe or stop on reconnect. A later process always starts above the earlier one's range. Pinned by `TestPublish_EpochSeedsEventIDs` and `TestPublish_LaterProcessHasHigherIDs`.
- Delivery is lossy by design: `recordDropForFullSubscriber` drops rather than blocking `Publish`, because the ring plus `Replay` is the recovery path. `Dropped()` makes that backpressure observable — lossy, but never silent.
- `warnIfEventsWereEvictedBeforeResume` surfaces a resume that lands after an already-evicted id. The client still receives only the retained tail, but a silent gap otherwise looks exactly like a clean resume. `afterID == 0` means "from the beginning" and expects no gap.
- `NoopPublisher` discards every event, so callers without a real `Publisher` default to it instead of nil-guarding every `Publish`.

## HTTP error translation (`httputil/errors.go`)

`StatusError` is satisfied **structurally** — a domain or service error carries its own HTTP status and a client-safe message without importing `httputil`, keeping `net/http` out of the inner rings. `HandleServiceError` is the single domain-error → HTTP translation point and replaced the per-handler `errors.Is`/`As` ladders: a `StatusError` in the chain renders with its declared status and message; anything else is logged and returned as a generic 500, so internals never reach the client.

`statusWriter.Flush` forwards to the underlying `ResponseWriter` so streaming handlers keep working through the logging wrapper — the SSE endpoint at `/v1/events` type-asserts `http.Flusher`, and without the forward that assertion fails and SSE 500s. `WithCorrelationID` exists for synthetic request paths (the Mission Control re-run inspector) that need correlation-keyed telemetry without passing through the HTTP middleware.

## HTTP record/replay (`httptrace/`)

`Recorder` is a DEBUG/observability tool that buffers full response bodies in memory — inject it **only** into throwaway clients (the `discoverytrace` CLI, tests), never the production path. `RoundTrip` restores both request and response bodies so the caller is unaffected.

`Replayer` is its other half: record once against live providers, then replay through the real pipeline for a deterministic offline run. Requests match on `(method, URL, request body)` — the fields a provider reconstructs deterministically from a fixed query and fixed credentials — and the body matters for POST/GraphQL providers whose query isn't in the URL. Repeated identical requests replay in capture order (FIFO) so pagination and retries reproduce faithfully. **A request with no matching exchange is a hard error, never a silent empty response**, so a missing fixture surfaces loudly and a deterministic eval can trust what it replayed. `Remaining()` reports exchanges never replayed; non-zero after a run means the replay diverged from the capture.

## Phonetics (`phonetics/`)

`DoubleMetaphone` is a simplified implementation covering common English pronunciation patterns for music artist/track names, and diverges from canonical double metaphone in places. Matching is symmetric — both sides use the same function — so the table-driven `rule` fields in `metaphone_test.go` are consistency anchors, not correctness claims: if one changes, every stored vocabulary metaphone key built from it goes stale. No branch currently emits a divergent alternate, so the second return value always equals the primary.

## Config flags (`config/config.go`)

Auth is JWKS-only — HS256 is not implemented, matching the Python behavior `Load` replaced, and a missing `SUPABASE_JWT_JWKS_URL` is a hard validation failure. `SUPABASE_ANON_KEY` is the publishable/anon key and is safe to expose to browsers; it lets the Mission Control page sign the operator in with email + password directly.

`OPERATOR_USER_ID` gates the Mission Control console **fail-closed** — unset denies everyone. `ALERT_NTFY_URL` is the alert push channel; empty means alerts are logged only, never pushed, and the topic must be non-guessable. `ALERT_ZERO_RESULT_THRESHOLD` (0 disables) pages the operator when zero-result searches in the last 24h exceed it — aggregate count only, never the query text.

Ranking and eval flags, and why each default is what it is:

- `EVAL_METER_ENABLED` (off) — the live smoke run hits real provider APIs and shares per-provider quota with user traffic, so it must be opted into deliberately.
- `TAIL_DEMOTION_ENABLED` (off) — demotes single-source UGC/scrobble results with no identity below corroborated ones. Flipped on for eval A/B before any production rollout. See `docs/brainstorms/2026-06-27-discovery-tail-noise-demotion.md`.
- `CROSS_KIND_PROMINENCE_ENABLED` (**on**) — among equally relevant results of different kinds, the more prominent entity (Deezer `nb_fan`/rank, log-compressed) sorts first, fixing bare-name artist-intent burial without touching track-vs-track order. The 2026-06-29 fixture A/B proved artist-intent top-1 +7.8pp and track-hard top-3 +3.4pp (−38 obscure-artist-on-top failures) with track-exact byte-identical. See `docs/solutions/2026-06-29-cross-kind-ranking-ties.md`.
- `BEHAVIORAL_RANKING_ENABLED` (off) — feeds the EventConsumer-derived satisfaction signal (play-to-completion +, skip-after-click −) into ranking as a within-tie input. A new ranking consumer, so it ships dark until eval A/B proves it on the hard corpus — same discipline as tail demotion.
- `BEHAVIORAL_CORPUS_PATH` (empty disables) — when set to a writable path, the nightly job materializes search→engagement labels (positive) plus `wrong_album` (hard negative) into the eval corpus format.
- `EXPLORATION_ENABLED` (off) / `EXPLORATION_RATE` — serves a randomized result order for a small fraction of searches (logged as exploration) so offline counterfactual eval has unbiased propensity data. The one user-facing behavior change, shipped dark so it needs no live sign-off.
- `IDENTITY_VERIFY_ON_PERSIST` (off) — the permanent identity-bridge fix (`docs/discovery-detail-pipeline.md` §7). MusicBrainz url-relations are not always correct and a wrong streaming link fuses two same-name artists (the wrong Deezer "Che"). When on, each learned streaming edge is checked against the artist's MusicBrainz release-groups before the bridge is persisted, and a non-overlapping edge is dropped, so the durable identity — and the detail fan-out / artwork that read it — never inherit the contamination. Runs off the request path; ships dark until its added MB/provider fetch load is measured.

`Config.LogValue` implements `slog.LogValuer` to redact secrets — it reports `has_*` booleans, never the values.

Knowledge base: `okf/backend/shared-infra.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
