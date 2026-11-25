`domainquality` decides how search and acquisition quality are graded.

- `New` builds the "Domain quality" bucket. It reads go-api's eval, acquisition and discography quality through `goapi`, and grades the worst of the three.
- The discography success rate is its key series.
- Its snapshot never blocks on a series write that is still in flight.
