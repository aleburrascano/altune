# internal/shared — router

Cross-cutting infrastructure: config, DB/Redis pools, structured logging + ring buffer, HTTP trace record/replay, httputil, textnorm, `UserId`. Per `docs/architecture.md`, this is the one layer domain code may import besides stdlib — and from it, only shared value objects like `UserId` plus the pure `textnorm` functions. Enforced by depguard (`services/go-api/.golangci.yml`, CI), which also blocks anything here from importing a feature package.

Gotchas:

- Redis: `NewClient` returns a **non-nil** client even when the ping fails — cache adapters must tolerate failing Redis calls at runtime, not just check for nil.
- `textnorm.NormalizeForMatch` is Unicode-aware by design; an AIDEV-NOTE records the ASCII-`\w` bug that zeroed CJK titles — don't "simplify" the symbol stripping.
- Domain errors reach HTTP via the structural `StatusError` interface and `HandleServiceError` — `domain/` never imports `net/http`.
- The log ring buffer captures at Debug regardless of stdout level; it feeds Mission Control's logs panel.

Knowledge base: `okf/backend/shared-infra.md` — read before structural work; update in the same commit when behavior it describes changes (pre-commit hook enforces).
