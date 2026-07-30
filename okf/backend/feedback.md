---
type: Bounded Context
title: Feedback
description: In-app bug reports and suggestions from sideloaded testers, filed as GitHub issues by the server so the device never holds the token.
resource: services/go-api/internal/feedback/
tags: [bounded-context, hexagonal, go-api, feedback, github, rate-limit]
verified_commit: e5bd4b0
---

Altune is sideloaded onto friends' and family's devices, and the point of this context is that a tester's reaction never has to become a text message or a shared doc. They tap one button in Settings, type a sentence, and it lands in the repo's issue tracker where a later session can pull it and fix it. The whole context exists to make that round trip cost the tester twenty seconds and the developer nothing.

**Why a server proxy and not a link.** The obvious cheap alternative is `Linking.openURL` to a pre-filled `issues/new?body=…`. It needs no backend at all — and it also drops a non-technical tester onto a GitHub login page, which is exactly the friction the feature exists to remove. The other alternative, shipping a token in the app, is not an alternative: a sideloaded APK can be unzipped. So the device posts to go-api and go-api holds the PAT. That decision is what buys the context its shape: an outbound adapter for GitHub, a port so the service never sees `net/http`, and a handler thin enough to be a translator.

**Domain** (`domain/report.go`): `Report{Reporter, Kind, Message, Diagnostics, SubmittedAt}`, built only through `NewReport`, which trims the message, gates its length (`MinMessageRunes` 10 / `MaxMessageRunes` 2000, counted in runes so an emoji-heavy report isn't measured in bytes), rejects a zero `Reporter`, and sanitizes diagnostics. `Kind` is a three-state enum (`KindBug` zero-value / `KindIdea` / `KindConfusing`) with two distinct string projections: `String()` is the wire and title form (`bug`), `Label()` is the GitHub label (`bug` / `enhancement` / `ux`). They are separate methods because the tester-facing vocabulary and GitHub's label vocabulary are free to drift — "confusing" is not a word GitHub has an opinion about, and `ux` is not a word a tester should have to pick.

`Diagnostics{AppVersion, Platform, OSVersion, Screen}` is client-supplied, so it is treated as hostile: `sanitized()` runs each field through `singleLine`, which collapses all whitespace runs to single spaces and truncates to 64 runes. That is not cosmetic. The issue body is markdown containing a table, and a field carrying a newline plus pipes could forge extra rows — the flattening is what makes the diagnostics table trustworthy. `Title()` takes only the first line of the message, truncates to 72 runes with an ellipsis, and prefixes the kind, so an issue list stays scannable no matter how long the report is.

`ValidationError` structurally implements `httputil.StatusError` with a plain int 400 — the domain never imports `net/http`.

**Ports**: `ports/issue_tracker.go` is a one-method port, `IssueTracker.Create(ctx, *Report) (IssueRef, error)`, returning `IssueRef{Number, URL}`. The number is what the confirmation dialog shows the tester, which is the whole reason the port returns anything at all rather than just an error — "sent" is weaker than "filed as #42".

**Service** (`service/submit_report.go`): `SubmitReportService.Execute` parses the kind, constructs the `Report`, and creates the issue. On success it logs `feedback.submitted` with the issue number, the kind and the reporter's user id.

**There is no rate limit, and adding one back would be a regression.** The first cut carried an in-memory per-user window of 5 an hour. It survived exactly one real session: the developer sat down to empty a backlog of bugs into the app and was told to try again in an hour, mid-dump. That is the failure mode worth remembering — the limit was reasoning about abuse in a product whose entire user base is the author's family, and it fired on the one usage pattern the feature exists to serve. Someone with fifteen things to say has fifteen things to say, and the moment they are willing to type them is the moment to take them.

What remains as a backstop is enough: GitHub enforces its own secondary limits on content creation, and the client sets `retry: false` on the mutation, so a failed send cannot turn into a silent retry storm. A runaway client loop is the only scenario the removed limiter would have caught, and it is better caught by not writing the loop.

**Adapters**: `adapters/github/issue_tracker.go` POSTs `/repos/{repo}/issues` with `Authorization: Bearer`, `X-GitHub-Api-Version: 2022-11-28`, labels `[kind label, from-app]`, and owns `renderBody` — the message, then a two-column diagnostics table, with `cell` escaping `|` and rendering an em-dash for an absent value. It carries its own `http.Client` with a 15s timeout rather than the process-shared `defaultLiveTransport`: that transport exists to hold per-host rate limits across discovery providers, and GitHub is not one of them. `WithBaseURL` exists only so the tests can point it at an `httptest` server. Failures are wrapped with the status and a capped 4KB slice of the response body, so a bad PAT is diagnosable from the log without dumping an unbounded body.

`adapters/handler/feedback_handler.go` exposes `POST /v1/feedback/reports` behind `auth.RequireUserID`, maps the snake_case request into `SubmitReportInput`, and returns 201 with `{issue_number, issue_url}`. `writeSubmitError` sets `Retry-After` when the error is a `RateLimitedError` before delegating to `httputil.HandleServiceError`, so a 429 always tells the client when to come back.

**The issue names the reporter by `UserId`, and by nothing else.** The first cut kept the reporter out of the public issue entirely and left the association in the server log — correct on paper, useless in practice: triaging a report meant grepping logs by issue number before you could even tell whether two complaints came from the same person. The `UserId` now rides in the diagnostics table. It is a Supabase UUID, not an identity: publishing it exposes no email, no display name, and no account access, and the opaque-id-in-a-public-repo trade was accepted deliberately for a handful of sideloaded testers.

The line that has not moved is **what else may go in**: no email, no display name, nothing a human reads as a person. `TestCreate_AttributesTheReportToItsReporter` asserts the id is in the body and *not* in the title — titles are read in lists, and a UUID in every title makes the issue list unscannable.

**Wiring** (`app/app.go`): `wireFeedback` returns nil when `HasIssueTracker()` is false (`GITHUB_ISSUE_REPO` + `GITHUB_ISSUE_TOKEN`), and `mountRoutes` skips the mount, so a deploy without an issue tracker 404s the route instead of 500ing inside it. This follows the same optional-dependency shape as the acquisition handlers. No database, no migration, no Redis.
