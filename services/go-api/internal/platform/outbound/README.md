`outbound` owns which outbound addresses the backend may dial.

`NewLiveTransport` retries only requests that are safe to send twice: GET, HEAD, OPTIONS, TRACE, PUT and DELETE. A POST is sent once unless its context went through `shared/retrysafe.Mark`, which a caller applies only to a POST that reads, such as a search or lookup.

`NewClient(timeout, transport)` is the one way to build an HTTP client: it refuses a zero timeout, and a nil transport means the net/http default, as it does for `http.Client`. `MustNewClient` is the same for a timeout that is a constant or validated config, and panics on a bad one. `NewTransport(opts)` builds a bare `http.Transport` from `TransportOptions`, with no proxy, no HTTP/2 upgrade and no per-host cap, for an adapter that needs its own pool settings. `DefaultTransport()` is the process-wide default transport. `NewLiveTransport` is the one that refuses non-public addresses and paces and retries.

Production code builds no `http.Client` or `http.Transport` of its own and does not use `http.Get`, `Head`, `Post`, `PostForm`, `DefaultClient` or `DefaultTransport`. Both checks run in `.golangci.strict.yml`: forbidigo for the calls and globals, and the gocritic ruleguard rule in `_rules/outbound.rules` for the `http.Client{}` and `http.Transport{}` literals. This package is their only home, and `*test` packages are out of both.
