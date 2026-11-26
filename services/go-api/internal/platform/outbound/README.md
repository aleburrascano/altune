`outbound` owns which outbound addresses the backend may dial.

`NewLiveTransport` retries only requests that are safe to send twice: GET, HEAD, OPTIONS, TRACE, PUT and DELETE. A POST is sent once unless its context went through `shared/retrysafe.Mark`, which a caller applies only to a POST that reads, such as a search or lookup.

`NewClient(timeout, transport)` is the one way to build an HTTP client: it refuses a zero timeout, and with a nil transport it dials only public addresses. Production code builds no `http.Client` or `http.Transport` of its own and does not use `http.Get`, `http.DefaultClient` or `http.DefaultTransport`. The ruleguard rule in `_rules/outbound.rules` states this, and it joins `.golangci.strict.yml` once no module has a hit left.
