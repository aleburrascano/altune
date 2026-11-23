# Lexicon review (2026-11): altune's code checked, concept by concept, against the Software Patterns Lexicon

## How it was made

- **Source:** 502 pages of [softwarepatternslexicon.com](https://softwarepatternslexicon.com), across 11 topics, one inventory per topic.
- **Code:** checked against gitea/main 7682acc25 on 2026-11-23, with `git grep` and `git show` only (no builds or tests). The errors, observability and principles inventories were read at 4cf08f4aa, four commits earlier. The synthesis re-read its key rows at 7682acc25 and marks them **[verified]**.
- **Verdicts:** each concept got one verdict with file:line evidence: done right, scattered, done wrong, missing (needed) or not needed. [brief.md](brief.md) is the instruction every topic followed.
- **Totals:** 380 rows. 172 of them are scattered, done wrong or missing, and need a change.

This is a dated snapshot. Its line numbers are as of 7682acc25 and are not kept current. Each open finding is tracked in a Gitea issue (see [Tracker](#tracker)).
The inventories cite working files (`pages/`, `tools/`, `principles/`, `door_usage.sh`) that were scratch and are not kept here.

## Files

- [synthesis.md](synthesis.md): the 13 themes (T1–T13), ordered by payoff against cost, plus the bugs found along the way, corrections between inventories, house rules the review found missing, and the totals per inventory.
- [brief.md](brief.md): how each inventory was made.
- Inventories, one table per topic (concept, lexicon pages, verdict, evidence, what would change):
  - [architecture.md](inventory/architecture.md)
  - [behavioral.md](inventory/behavioral.md)
  - [concurrency.md](inventory/concurrency.md)
  - [creational-structural.md](inventory/creational-structural.md)
  - [errors.md](inventory/errors.md)
  - [functional.md](inventory/functional.md)
  - [idioms.md](inventory/idioms.md)
  - [integration-data-security.md](inventory/integration-data-security.md)
  - [observability.md](inventory/observability.md)
  - [principles.md](inventory/principles.md)
  - [testing.md](inventory/testing.md)

## Tracker

One issue per theme, plus one for the findings outside the themes. Each issue lists its inventory rows and bugs as a checklist, and every one of the 172 rows is in exactly one issue.

| Theme | Issue |
|---|---|
| T1. One correlation id, from the client to the log line and back | #3704 |
| T2. One goroutine home: owner, bound, panic guard, detach and wait | #3705 |
| T3. One server error boundary | #3706 |
| T4. One outbound HTTP stack: client factory, retry, pacing, breaker | #3707 |
| T5. One database home: transactions, transient errors, pools, row scans | #3708 |
| T6. Whole-tree checks that ban the bypass | #3709 |
| T7. One telemetry pipeline: request outcome, field vocabulary, metrics registry | #3710 |
| T8. One client failure model | #3711 |
| T9. One home for client async plumbing: mutations, deadlines, cancellation, invalidation | #3712 |
| T10. Parse once at every boundary | #3713 |
| T11. One clock and one random source | #3714 |
| T12. Dependencies visible in constructors, with one composition root per app | #3715 |
| T13. One home for each domain rule | #3716 |
| Findings outside the 13 themes | #3717 |
