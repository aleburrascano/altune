`reliability` decides how backend errors and outages are graded.

- `New` builds the "Reliability" bucket. `Start` runs a poller of go-api's health, and `Wait` joins it.
- Its key series is latency in milliseconds. A reason from the admin endpoint outranks a degraded poll, and a down dependency outranks a degraded poll.
- `Rings` hands out its history ring and the poller's ring, which `app` restores from `history` after a restart.
