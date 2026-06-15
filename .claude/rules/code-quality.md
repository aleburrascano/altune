---
paths:
  - "services/go-api/**"
  - "apps/mobile/src/**"
---

# Code quality invariants

- Every type has one reason to change. Name that reason or split it.
- Functions: max 10 lines. Types: max 50 lines. Extract when longer.
- No `else` when early return works. Prefer guard clauses.
- No abstractions until third duplication (Rule of Three). A little duplication is better than the wrong abstraction.
- Wrap domain primitives in value objects (IDs, emails, money). No raw strings or ints for domain concepts.
- Tell, Don't Ask: command objects rather than query-then-decide externally.
- Dependencies point inward only (domain <- application <- adapters). Never reverse.
- Let design patterns emerge from refactoring. Don't force them.
- Detect complexity: change amplification (small change = many files), cognitive load, unknown unknowns. Fight with YAGNI, KISS, DRY (after Rule of Three).
- Every public API has a clear contract: what it accepts, what it returns, what errors it can produce.
