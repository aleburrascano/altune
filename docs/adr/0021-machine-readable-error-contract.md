# ADR-0021: Add a stable machine-readable `code` to the service error contract, purely additively

- **Status:** Accepted
- **Date:** 2026-09-07
- **Deciders:** solo + Claude
- **Context tags:** [pattern | policy | layer]

## Context

`HandleServiceError` (`services/go-api/internal/shared/httputil/errors.go:19-28`) is the one funnel every service/domain failure passes through. For any `StatusError` it writes `ErrorResponse{Detail: se.Error()}` — the client receives `{"detail":"track not found"}` and nothing else. The domain `Error()` string *is* the wire contract: a caller can only branch on HTTP status plus English prose (`ValidationError.Error()` returns the raw `Message`; `CodedError.Error()` returns `Msg`). Renaming "track not found" to "Track not found" would silently break any client keying off it. This violates the **Error contract** standard (`~/.claude/workflow/robustness-domains.md`, Classification: "each failure is classified … and the classification is what the caller branches on") and `code-style.md` ("expected failure is a return value — callers branch on a code, never on a message string").

The auth path already solved this locally: `rejectToken` sends `rejectResponse{Detail, Reason}` — a typed `reason` (`ReasonMissing`, `ReasonMalformed`, the `InvalidTokenError.Reason` values) *alongside* the human `detail` (`auth/middleware.go:83-95`). The service path owes the same programmatic discriminator.

This is a **wire-contract change and therefore BREAKING to design carelessly**, which is why it is ADR-first (issue #99). The error body is consumed across the mobile app, so the shape must be fixed and its additivity proven before any code ships. This ADR is the decision record only; it writes no Go.

## Decision

**Add one field, `code`, to `httputil.ErrorResponse`, appended after the existing `detail`. Nothing is renamed, removed, retyped, or reordered.**

New body shape for every service/domain error:

```json
{ "detail": "track not found", "code": "catalog.track_not_found" }
```

- `detail` — **unchanged, byte-for-byte.** Same field, same JSON tag, same value (`se.Error()`). Human-facing, free to reword; no client may branch on it.
- `code` — **new, additive.** A stable, lowercase, dot-namespaced (`<context>.<slug>`) string that never changes once shipped. This is the value callers branch on.

`code` is produced by a narrow optional interface in `httputil`, checked with `errors.As` inside `HandleServiceError`:

```go
type ErrorCoder interface{ ErrorCode() string }
```

Resolution, in order, so **every** emitted body carries a non-empty `code` (Totality — the funnel is total over its error input):

1. The error implements `ErrorCoder` → use its `ErrorCode()` (the named sentinels below).
2. Else, a `StatusError` without an explicit code → a status-derived fallback code (`400→"bad_request"`, `404→"not_found"`, `409→"conflict"`, other → `"error"`).
3. The generic non-`StatusError` 500 path (`InternalError`) → `"internal"`.

Adopting `ErrorCoder` on the domain error types is the follow-up implementation (issue #99), not this ADR. The fallback in step 2 means the field is present and stable from day one even before every type opts in, so no error is ever codeless during the rollout (Migration overlap: old code emits fallback codes, new code emits explicit ones, both valid).

**Field name is `code`, not `reason`.** Auth's existing `reason` stays exactly as it is — a separate response struct (`rejectResponse`), a separate and narrower taxonomy (token-rejection reasons), untouched by this ADR. Reusing `reason` for the general service taxonomy would overload one name across two vocabularies; `code` is the general machine discriminator, `reason` remains auth's local one. Unifying auth onto `code` (additively — auth would gain `code` beside its `reason`) is explicitly deferred, not decided here.

### It is purely additive — the confirmation

- **No field renamed, removed, retyped, or reordered.** `detail` keeps its tag, type, position, and value.
- A client deserializing the old `struct { Detail string }` still succeeds — encoding/json ignores the unknown `code`. Old clients keep working with zero change.
- The current Altune mobile client is even safer than "ignores the extra field": `apiFetch` **discards the error body entirely** — on a non-2xx it throws `new ApiError(response.status, "API ${path} returned ${response.status}")` (`apps/mobile/src/shared/api-client/index.ts:66-68`) and never reads `detail`. The "~40 mobile files" that reference `detail` are UI **detail screens** (album/artist/track `detail.tsx`), unrelated to the error body. So adding `code` cannot break the existing client: it reads neither the old field nor the new one.

### Code taxonomy (class → stable code)

The task is to map the domain error **classes** to stable string codes. There are two shapes of `StatusError` today:

**Named sentinels** — one distinct code each (they already carry identity as package-level vars):

| Error (source) | HTTP | `code` |
|---|---|---|
| `ErrTrackNotFound` (`catalog/service/errors.go:6`) | 404 | `catalog.track_not_found` |
| `ErrPlaylistNotFound` (`catalog/service/errors.go:7`) | 404 | `catalog.playlist_not_found` |
| `ErrAudioNotAvailable` (`catalog/service/errors.go:8`) | 404 | `catalog.audio_not_available` |
| `ErrTrackAlreadyInPlaylist` (`catalog/domain/errors.go:11`) | 409 | `catalog.track_already_in_playlist` |

**Class-level codes** — the value-carrying error types map to one code per class, since the type carries a free-text `Message` but no per-instance identity. This is the honest granularity: a caller branches on "this was a validation failure," and reads `detail` only to display it.

| Error class (sources) | HTTP | `code` |
|---|---|---|
| `catalog.ValidationError` (`catalog/domain/errors.go:13`; raised in `track.go`, `playlist.go`, `library_lens.go`, `service/*`) | 400 | `catalog.validation_error` |
| `feedback.ValidationError` (`feedback/domain/report.go:12`) | 400 | `feedback.validation_error` |
| `playback.ValidationError` (`playback/domain/queue_state.go:12`) | 400 | `playback.validation_error` |
| `discovery.invalidEventError` (`discovery/service/record_event.go:30`) | 400 | `discovery.invalid_event` |
| `catalog.CodedError` (base, no named sentinel) | its `Status` | status-fallback (`bad_request`/`not_found`/`conflict`/`error`) |
| Unhandled non-`StatusError` (`InternalError`) | 500 | `internal` |

Finer-grained validation codes (e.g. `catalog.playlist_name_too_long` distinct from `catalog.track_title_required`) would require adding a `Code` field to each `ValidationError` construction site. That is a strictly additive follow-up — new codes only ever *subdivide* `*.validation_error`, never rename it — and is out of scope here.

### Mobile adoption

Adoption on the client is a **follow-up ticket, not part of #99's backend slice** (the backend is safe to ship first precisely because the current client ignores the body). The client change, when taken:

1. `apiFetch` reads the JSON body on a non-2xx (guarded — a truncated/empty body already becomes a `NetworkError` via `readBody`) and threads `code` into `ApiError`, e.g. `new ApiError(status, message, code?)`. `ApiError.code` is optional so a body without one (older server, non-JSON error) leaves it `undefined` — the client degrades to status-only branching, exactly as today.
2. Callers that today branch on `error.status` gain the option to branch on `error.code`. No caller is *forced* to change; `code` is additive on the client too.
3. `apps/mobile/src/shared/api-client/types.ts` (hand-maintained wire types, per that dir's `CLAUDE.md`) gains the `code` field in the same client change.

Until that ticket lands, the client keeps working unchanged — the backend change is independently shippable.

### Characterization-test plan (pin the body before and after)

Goal: prove `detail` is byte-stable and `code` is present and branchable. Tests live with `HandleServiceError` (`internal/shared/httputil`) and the catalog handler tests that already exercise these errors.

- **Before (golden, current shape).** For each error path — `ErrTrackNotFound`, a `ValidationError`, the generic 500 — capture the exact current JSON: `{"detail":"track not found"}`, `{"detail":"<msg>"}`, `{"detail":"internal server error"}`. Commit these as the pre-change baseline (Regression category).
- **After — `detail` unchanged.** Decode each new body into the **old** `struct { Detail string }` and assert the `Detail` value is byte-identical to the golden. This is the additivity proof: the old contract still parses and still carries the same string.
- **After — `code` present and correct.** Assert the new body contains the mapped `code` from the taxonomy table, and that a caller can switch on it (the issue's "a test asserts a caller can branch on the code"). One negative case: an error with no explicit code emits the status-derived fallback, never an empty string (Totality).
- **Auth untouched.** Pin `rejectResponse` bodies (`{"detail":"…","reason":"…"}`) so the refactor cannot accidentally alter the auth path this ADR leaves alone.

Test taxonomy verdicts (Regression, plus Characterization/Golden and the Totality/Classification surface) are recorded in `okf/testing/<slice>.md` by the implementing ticket, per the project testing rule.

## Alternatives considered

| Alternative | Why not |
|---|---|
| Reuse auth's `reason` field for the service taxonomy | Overloads one field name across two distinct vocabularies (token-rejection reasons vs. general error codes). Keeping `code` general and `reason` auth-local keeps each taxonomy legible; auth can gain `code` additively later. |
| Replace `detail`'s free text with the code (one field, machine-only) | Breaking: renames the contract's only field's meaning, and drops the human string clients/logs display. Fails the additive requirement outright. |
| Nest under an `error` object (`{"error":{"code":…,"detail":…}}`) | A full reshape — every existing `detail` reader breaks, no old client survives. The whole point of #99 is to avoid a reshape; a flat additive field achieves the same machine-readability with byte-stable existing fields. |
| Add `Code()` to the `StatusError` interface (mandatory, not optional) | Forces every current implementer to add `Code()` in one commit and gives no graceful overlap. The optional `ErrorCoder` + status-fallback lets types opt in incrementally while every body still carries a code from day one. |
| Per-instance code on `ValidationError` now (fine-grained from the start) | Larger change touching ~15 construction sites, and orthogonal to fixing the contract shape. Class-level codes satisfy "branch on a code"; subdividing later is additive and needs no further ADR. |
| Ship code + mobile change in lockstep | Unnecessary coupling: the current client ignores the body, so the backend is independently safe. Lockstep would gate a safe backend change on client work for no compatibility reason. |

## Consequences

### What becomes easier
- Clients (and Mission Control, and tests) branch on a stable `code` instead of English prose — the Error-contract standard is met at the one funnel every service error passes through.
- Reworded `detail` copy stops being a silent breaking change; the machine contract and the human message decouple.
- New coded errors extend the taxonomy additively — a new `code` is new information, never a rename.

### What becomes harder
- The taxonomy is now a maintained contract: a shipped `code` is frozen. Renaming one is a breaking change requiring its own ADR. Adding is free; changing is not.
- Two discriminators coexist for a while (`code` service-wide, `reason` in auth) until the deferred unification — a reader must know which path they are on.

### What we're committing to (and the cost to reverse)
- A flat, additive `code` field on the JSON error body, and the string values in the taxonomy table as a stable public contract. Reversing the *shape* is cheap (drop the field — no reader depends on it yet); reversing a *published code string* is not, once a client branches on it. That asymmetry is the reason this is ADR-first.

## Implementation notes

Execution is issue #99 (backend slice): add `ErrorCoder` to `httputil`, make `HandleServiceError` resolve and emit `code` with the fallback ladder, add `ErrorCode()` to the four named sentinels and the four value classes per the taxonomy, and land the characterization tests above. Update `services/go-api/internal/shared/httputil` behavior notes in `okf/backend/shared-infra.md` and the affected `okf/testing/<slice>.md` in the same commit (pre-commit hooks enforce). Mobile adoption is a separate follow-up ticket as described above. No code ships with this ADR.

## Vault references

Patterns and principles this decision rests on:

- `~/.claude/workflow/robustness-domains.md` — Classification ("the classification is what the caller branches on"), Totality (the funnel is total over its error input), Migration (old/new codes coexist during rollout).
- `~/.claude/workflow/code-style.md` — "Expected failure is a return value … callers branch on a code, never on a message string."
- `.claude/rules/code-quality.md` — "Every public API has a clear contract: what it accepts, what it returns, what errors it can produce."

## Related

- Issue: #99 (machine-readable error code — ADR-first; this ADR precedes its implementation)
- Predecessor pattern: `auth/middleware.go` `rejectResponse` (typed `reason` beside `detail`) — the service path generalizes it as `code`
- Wire funnel: `services/go-api/internal/shared/httputil/errors.go` (`HandleServiceError`, `ErrorResponse`)
- Client contract: `apps/mobile/src/shared/api-client/` (`index.ts` `apiFetch`, `errors.ts` `ApiError`, `types.ts`) — adoption is a follow-up ticket
