# internal/app — composition root — router

The only place adapters are chosen and wired into ports. Also home to the shared rate-limited HTTP transport, the SSE seam, and the eval/re-run/inspector operator surfaces.

Invariants:

- `BuildSearchService` is the single construction site for the search pipeline — production, eval CLI, and eval meter must all go through it so eval never drifts from what users see.
- Eval/synthetic searches always get a nil event store; exploration is never wired on the `rankingOnly` path.
- `defaultLiveTransport` is process-shared on purpose: per-host rate limits only hold if every provider client shares one limiter. Never give a provider its own transport.
- SSE: never 204 an empty replay (EventSource stops reconnecting); emit `resync` on ring gaps.
- Nil-tolerant degradation is the house style: nil Redis/MB/audio-store switch features off, never fail startup.

Knowledge base: `okf/backend/app-wiring.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
