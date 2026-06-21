# acquire-soundcloud — implementation plan

Spec: docs/specs/acquire-soundcloud/spec.md

Scope is a **single adapter file**: `internal/acquisition/adapters/ytdlp/searcher.go`.
No domain/port/service/schema change. The existing `SelectBestCandidate` already
routes a non-Topic SoundCloud candidate correctly (Topic-first), so selection
needs no change — slice 2 just pins that with a test.

Vault: [vault: wiki/concepts/Anti-Corruption Layer Pattern.md] — the searcher is
the ACL; adding a source stays inside it.

## Slices

### Slice 1: dual-engine searcher (YouTube + SoundCloud) behind a testable runner seam
- Acceptance criteria: AC#1, AC#2, AC#3, AC#4, AC#5
- Files:
  - `services/go-api/internal/acquisition/adapters/ytdlp/searcher.go` — extract the
    yt-dlp search invocation into an injectable runner field
    `runSearch func(ctx, searchSpec string) ([]ports.AudioCandidate, error)`
    (default = the real `exec.CommandContext` path, unchanged args). `Search`
    loops over the engine prefixes `["ytsearch5:", "scsearch5:"]`, calls
    `runSearch` per engine, merges + dedups by URL. One engine error → log + skip
    (keep the other's results); both error → return an error. `Download` unchanged.
  - `services/go-api/internal/acquisition/adapters/ytdlp/searcher_test.go` (new).
- Domain/application/adapter: adapter-only. `AudioSearcher.Search` contract
  unchanged (still takes a plain query).
- Failing test first: `TestYtDlpAudioSearcher_Search_QueriesBothEngines` — inject
  a fake runner that records search specs and returns canned candidates per
  engine; assert (a) both `ytsearch5:<q>` and `scsearch5:<q>` were issued, (b)
  union returned, (c) duplicate URL collapsed, (d) one-engine-error still returns
  the other, (e) both-error returns an error.
- Verify: `cd services/go-api && go test ./internal/acquisition/... -run Search -count=1`

### Slice 2: pin selection behaviour for SoundCloud candidates (no production change)
- Acceptance criteria: AC#6, AC#7
- Files:
  - `services/go-api/internal/acquisition/service/matching_test.go` (new) —
    table tests over `SelectBestCandidate`.
- Application layer: characterization tests only (the spec asserts the existing
  gates already do the right thing — lock it so a future selection refactor can't
  silently break the SoundCloud path).
- Failing test first: `TestSelectBestCandidate_SoundCloudFillsGap` —
  (a) only a SoundCloud-style candidate (non-Topic channel, good title match,
  matching duration) passes the identity gate → it is selected;
  (b) a YouTube `- Topic` candidate + a SoundCloud candidate both present → the
  Topic candidate is selected (Topic-first preserved).
- Verify: `cd services/go-api && go test ./internal/acquisition/... -run SelectBestCandidate -count=1`

## Final verification (whole feature)
- `cd services/go-api && go test ./internal/acquisition/... -count=1 && go vet ./internal/acquisition/... && go build -o ./tmp/api.exe ./cmd/api`
- **Not verifiable in this environment** (honest disclosure, same limits as the
  existing pipeline): a real `scsearch5:` hit + SoundCloud-URL download + OCI
  store requires yt-dlp, network access to SoundCloud, and OCI credentials. The
  unit tests cover the dual-engine merge and the selection routing; the live
  download/store path is exercised by the already-built YouTube pipeline and the
  identical yt-dlp `Download(url, …)` call (SC URLs use the same code path).

## Risks
- **SoundCloud title noise** drags identity below the 60 gate → real match
  rejected. Acceptable v1 failure mode (fail > wrong audio); title cleanup is
  deferred. (spec Risks)
- **2× subprocess cost** per acquisition — acceptable for a once-per-track
  background job under a 10-min ceiling; concurrency deferred. (spec Risks)
- **Bootleg re-upload wins** only when no YouTube Topic candidate qualifies; the
  identity + duration gates still apply. (spec Risks)

## ADR candidates
- None. Adapter-internal change; no new dependency, port, or domain type.
