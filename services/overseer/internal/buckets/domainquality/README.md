`domainquality` decides how search and acquisition quality are graded.

- `New` builds the "Domain quality" bucket. It reads go-api's eval and acquisition results through `goapi` and reports a score and a rate for each.
- Its snapshot never blocks on a series write that is still in flight.
