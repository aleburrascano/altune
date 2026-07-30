# Feedback context — router

In-app bug reports and suggestions from sideloaded testers, filed as GitHub issues. The server holds the token; the device never sees it.

Layout:

- `domain/report.go` — `Report`, `Kind` (`bug` / `idea` / `confusing`) and its GitHub label mapping, `Diagnostics`, `NewReport`, `Title`, `ValidationError`.
- `ports/issue_tracker.go` — `IssueTracker`, `IssueRef`.
- `service/submit_report.go` — `SubmitReportService`. `service/rate_limiter.go` — in-memory per-user `RateLimiter` and `RateLimitedError`.
- `adapters/github/issue_tracker.go` — GitHub REST v3 issue creation and the issue-body markdown.
- `adapters/handler/feedback_handler.go` — `POST /v1/feedback/reports`.
- Tests: `domain/report_test.go`, `service/submit_report_test.go`, `adapters/github/issue_tracker_test.go`, `adapters/handler/feedback_handler_test.go`.

Dependencies: `internal/auth`, `internal/shared` (`UserId`, `httputil`), `chi`. No database, no Redis.

## Rules

- The issue body carries the reporter's `UserId` and nothing else about the account — never an email, never a display name.
- Keep the reporter out of the issue title; titles are read in lists.
- Flatten and cap every diagnostics field before it reaches the issue body; escape `|`.
- Check the rate limit only after the report validates — a rejected report must not spend quota.
- Keep the limiter in memory; never add a store for it without an ADR.
- Mount the route only when `HasIssueTracker()` — an unconfigured deploy must 404, not 500.
- Set `Retry-After` on every 429.

Why each rule exists: `okf/backend/feedback.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
