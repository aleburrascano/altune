`backendperf` decides how backend latency and throughput are graded.

- `New` builds the "Back-end performance" bucket. It reads go-api's metrics through `goapi`, ranks the routes slowest first and grades the whole bucket by the worst one.
- A route that is slow turns the bucket amber or critical, and a route with enough failing responses shows its error rate. One error on an idle route does not grade critical.
- When go-api cannot be read, the bucket degrades to source-down instead of failing.
