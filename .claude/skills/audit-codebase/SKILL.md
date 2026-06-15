---
name: audit-codebase
description: >
  Sweep the codebase against all rules — frontend, backend, or both.
  Reads every file, audits against loaded rules (conventions, quality,
  resilience, testing, security, accessibility), reports findings by
  severity, optionally fixes them. Use every 3-4 days or after shipping
  a feature batch.
argument-hint: "[frontend | backend | all] [--fix]"
---

# Codebase Audit

Systematic sweep of source files against all project rules. Reading files triggers path-scoped rules to load automatically — no manual wiring needed.

## Arguments

- `frontend` — sweep `apps/mobile/src/` (features/, shared/, app/)
- `backend` — sweep `services/go-api/` (internal/, cmd/)
- `all` — both frontend and backend (default if no argument)
- `--fix` — apply fixes after reporting (default: report only)

## Workflow

### Phase 1: Discovery

Enumerate every source file in the target scope. Group by area:

**Frontend** (`apps/mobile/src/`):
- Each feature folder: `features/detail/`, `features/discover/`, `features/library/`, `features/playback/`, etc.
- Shared code: `shared/ui/`, `shared/api-client/`, `shared/lib/`
- App routing: `app/`

**Backend** (`services/go-api/`):
- Each bounded context: `internal/catalog/`, `internal/discovery/`, `internal/acquisition/`, `internal/auth/`
- Shared infrastructure: `internal/shared/`, `internal/app/`
- Entry points: `cmd/api/`

Report the file count per area before starting.

### Phase 2: Audit (area by area)

For each area, read EVERY file. Do not skip any. As you read, the path-scoped rules load automatically:
- Frontend files trigger: rn-conventions, rn-quality, rn-production, typescript-frontend, code-quality
- Backend files trigger: go-conventions, go-patterns, go-production, domain-layer, application-layer, adapters-layer, code-quality
- Test files additionally trigger: tests rules

For each file, check against ALL loaded rules. Look for:

**Conventions violations:**
- Naming (Go: MixedCaps, receivers; TS: PascalCase components, camelCase hooks)
- File structure (one component per file, proper exports, import ordering)
- Style (early returns, no else, function length, type strictness)

**Quality issues:**
- Code duplication across files (extractable hooks, shared utilities, common patterns)
- Abstraction opportunities (Rule of Three: 3+ duplications → extract)
- Missing value objects / primitive obsession
- SRP violations (types doing too much)
- Dead code, unused imports, unused exports

**Performance issues:**
- Frontend: inline functions in lists, missing memoization, ScrollView for dynamic lists, missing keyExtractor, large bundle imports
- Backend: missing timeouts, unbounded operations, N+1 patterns

**Resilience gaps:**
- Frontend: missing loading/error/empty states, no offline handling, duplicate submission possible, missing accessibility labels
- Backend: missing context propagation, bare error catches, no retry/circuit breaker on external calls, missing observability

**Testing gaps:**
- Missing test files for behavioral code
- Tests testing implementation instead of behavior
- Missing edge cases visible from the code

**Security concerns:**
- Hardcoded secrets, insecure storage, unvalidated input, missing HTTPS

**Architecture violations:**
- Hexagonal boundary breaches (domain importing adapters, cross-feature imports)
- Feature isolation violations (feature A importing from feature B)
- Shared code with only 1 consumer (premature extraction)

### Phase 3: Report

Group findings by severity:

```
## 🚨 Critical (fix now)
Findings that would cause bugs, security issues, or data loss.

## ⚠️ High (fix this sprint)
Significant quality issues, missing resilience, architecture violations.

## 📋 Medium (fix soon)
Convention violations, missing tests, abstraction opportunities.

## 💡 Low (consider)
Style improvements, minor optimizations, nice-to-haves.
```

Within each severity, group by area (feature folder or bounded context).

For each finding, report:
1. **File** and line range
2. **Rule violated** (which rule markdown it comes from)
3. **What's wrong** (specific, not vague)
4. **Suggested fix** (concrete, not "improve this")

End with a summary table:

```
| Area | Critical | High | Medium | Low | Files |
|------|----------|------|--------|-----|-------|
| features/library/ | 0 | 2 | 5 | 3 | 12 |
| features/playback/ | 1 | 1 | 3 | 2 | 8 |
| ... | ... | ... | ... | ... | ... |
| TOTAL | 1 | 3 | 8 | 5 | 20 |
```

### Phase 4: Fix (only if `--fix` was passed)

Work through findings by severity (critical first). For each fix:
1. Make the change
2. Run the relevant type checker / linter if available
3. Move to next finding

Commit fixes per area: `refactor(<scope>): apply audit findings`

## Context management

If the codebase is too large for one pass:
1. Complete one area fully before moving to the next
2. Report findings for the completed area
3. Ask the user to continue to the next area (this lets context reset if needed)

## What this skill does NOT do

- Rewrite working code for style preferences alone
- Add features or change behavior
- Modify tests unless they violate the sacred-tests rule (implementation detail testing)
- Touch files outside the specified scope
