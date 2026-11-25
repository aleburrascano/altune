`cost` decides how cloud spend is graded against its budget.

- `New` builds the "Cost" bucket from a `SpendReader` (the tenancy's month-to-date spend, from `oci`) and go-api's provider usage.
- It records one point on a new spend reading and nothing on a repeated identical poll. Its key series is the month-to-date spend, and the daily figure resets when a new billing period starts.
- It records provider calls per provider.
