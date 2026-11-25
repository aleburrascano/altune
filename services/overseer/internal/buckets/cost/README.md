`cost` decides how cloud spend and provider calls are graded.

- `New` builds the "Cost" bucket from a `SpendReader` (the tenancy's month-to-date spend, from `oci`) and go-api's provider usage.
- Its severity comes from provider outcomes: critical when no call succeeded, a warning when failures outnumber successes.
- It records one point on a new spend reading and nothing on a repeated identical poll. Its key series is the month-to-date spend, and the daily figure resets when a new billing period starts.
