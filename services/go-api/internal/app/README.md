`app` has one job: build every module in order, hand each its part of the settings, schedule the background jobs and run the server. It decides nothing a module owns.

## Files

- `app.go` builds everything in order and runs it. `config.go` reads the settings once and hands each module its part.
- `library_wiring.go`, `search_wiring.go`, `download_wiring.go`, `playback_wiring.go` and `platform_wiring.go` wire one module each. `platform_wiring.go` covers auth, the alert monitor, the observe endpoints and `/health`.
- `jobs.go` schedules the jobs and holds the runner, the job names and the leader election wiring. `shutdown.go` stops them in order. `server.go` builds the router and the HTTP server.
- `bridges/` holds one package per module pair, named `<to>from<from>`: it serves the ports of `<to>` from `<from>`. Data crossing a bridge is a shared type where one exists, and a field mapping only where the two modules each own their type.

## Must not

- Decide anything a module owns. An alert rule, a metric definition, the erase or prune of a table's rows and the choice of a notifier live in the module that owns the decision; `app` calls them and schedules them. `lintdoors --shape` fails when `app` holds a file or folder not listed here.
- Hold a second wiring file for a module or a package beside `bridges/`.
- Read the environment: `config.go` takes the one `config.Config` that `shared/config` built.

`export_test.go` exposes the unexported wiring names to the tests, which all live in `package app_test`.
