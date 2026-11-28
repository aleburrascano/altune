`monitoring` owns what operators see about the backend's health, metrics and alerts.

It owns the table `client_events`. Only `monitoring` writes it. Other modules may read it.

## Must not

- Import a feature or another platform module (`outbound`, which its notifiers use, is the one exception). `search` and `download` data reaches it through `app/bridges` (`monitoringfromsearch`, `monitoringfromdownload`), and what decides when to page (the alert conditions) and what the live metrics map holds stay in `app/alerts` and `app/livemetrics` until they can be expressed through its ports.
- Put a domain type on the wire. Wire shapes live in `internal/adapters/handler/dto.go` and its errors map in `errors.go`. The alert monitor status, the eval meter status and the tap events keep their json tags in `internal/service/` because `app/livemetrics` and the observe stream serialize them as they are.
- Read config: `app` passes the notifier and the intervals; the observe stream bounds default unless `Deps.Streams` sets them.

## Shape

The door is `monitoring.go`: the observe handler, the alert monitor, the event tap and feed, the eval meter, the notifiers and the client event routes. Under `internal/`: `domain/` (alert, client event, `errors.go`), `ports/ports.go`, `service/` (alert monitor, client event recorder, event tap, feed and broadcaster, eval meter), `adapters/handler/` (`routes.go`, `dto.go`, `errors.go` and the reads and streams), `adapters/notifier/` (webhook and email), `adapters/persistence/client_event_repo.go` and `adapters/memory/client_event_repo.go`, with `contracttest/` holding the suite that runs against both (the Postgres half runs with `INTEGRATION=1`). `monitoringtest/` holds the names `app`'s tests need.
