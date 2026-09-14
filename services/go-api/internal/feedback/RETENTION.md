# Feedback — Data Retention & Deletion Policy

This document records the retention/deletion story for the personal data that
feedback submits, and the explicit design decision about erasure of feedback that
leaves the system as a GitHub issue. It is the written companion to catalog's
retention precedent (`internal/catalog/RETENTION.md`).

## What personal data feedback holds

Feedback keeps **no durable store of its own**. A submission is turned into a
GitHub issue and then forgotten by the service. Two kinds of personal data leave
the process:

- **The GitHub issue** — created by
  `adapters/github/issue_body.go` (`renderBody`). Its body carries:
  - the reporter's free-text `Message`, verbatim (rendered inside a code fence so
    GitHub shows it literally — no @mentions, links, images, or forged tables), and
  - a diagnostics table whose **Reporter** row is the reporter's durable
    `shared.UserId` UUID, plus app version, platform, OS version, screen, submit
    time, and the request correlation ID.
- **A structured log line** — `service/submit_report.go` (`create`) emits
  `event=feedback.submitted` carrying the created `issue` number, the `kind`, and
  the reporter `user_id`.

The service holds only two ephemeral, in-memory structures — the submission
admission (rate-limit) window and the idempotency replay cache. Both are
time-bounded, are never persisted, and vanish on restart; neither is a durable
record of personal data.

## Decision: feedback issues are support correspondence, outside the app's automated erasure

A feedback GitHub issue is treated as **operational support correspondence**, in
the same class as an email a user sends to support. It deliberately lives **outside
the app's automated, per-user in-app deletion guarantees**, for the same reasons
catalog defers a bulk erasure method (see `internal/catalog/RETENTION.md` and
account-deletion orchestration ticket #430):

- The record lives in a **third-party system (GitHub)** the service does not own
  and cannot transactionally cascade against. An in-app "delete my account" path
  cannot atomically guarantee removal of an external issue.
- Feedback is **operational triage data** (bug reports, ideas), not a user-facing
  asset the user manages inside the app; there is no in-app surface that lists or
  owns these issues.
- Building an automated GitHub-erasure pipeline is **explicitly out of scope** for
  this decision (it belongs with the #430 orchestration if that is ever built);
  adding one now would be speculative capability with no caller.

## Erasure is still possible on request (locatability)

Treating feedback as support correspondence does **not** mean a user's submissions
are unfindable. On a verified erasure request an operator can locate and delete a
user's feedback issues by their reporter UUID through **two independent paths**,
so neither is a single point of failure:

1. **The issue body itself.** The reporter UUID is written into every issue's
   **Reporter** row as plain text, so GitHub's own search finds them directly, e.g.
   `gh issue list --search "<reporter-uuid> in:body"`.
2. **The application log.** Every submission logs `event=feedback.submitted` with
   the `issue` number against the reporter `user_id`, so the reporter→issue mapping
   is recoverable from logs for as long as they are retained, independent of
   GitHub search.

Deletion, once located, is a manual operator action (`gh issue delete`), matching
the "support correspondence" classification: erasure is honoured on request rather
than automated on every account deletion.

## Minimisation already in place

The submission path is deliberately narrow so the permanently-forwarded record
carries as little as possible:

- `Message` is length-bounded (`MinMessageRunes`/`MaxMessageRunes`) and the reporter
  is told it is free text — the service adds no extra identifiers beyond the
  reporter UUID and the coarse diagnostics above.
- Diagnostics are single-lined and truncated (`maxDiagRunes`) and the title is
  bounded (`maxTitleRunes`); no email, IP, device id, or account name is forwarded.
- The reporter identifier is the opaque `shared.UserId` UUID, not an email or
  handle, so the issue cannot be tied to a real-world identity without the app's
  own user table.

If future feedback needs a stronger guarantee than manual-on-request erasure, the
chosen shape is to fold GitHub-issue deletion into the #430 account-deletion
orchestration, driven by the reporter→issue mapping this document relies on. Until
then, this decision is the retention policy of record.
