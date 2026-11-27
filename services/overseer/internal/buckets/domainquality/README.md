`domainquality` decides how search and acquisition quality are graded.

- `New` builds the "Domain quality" bucket. It reads go-api's eval, acquisition and discography quality through `goapi`, and grades the worst of the three.
- The discography success rate is its key series.
- It computes the "get a song" SLO, that 90% of acquisitions finish as succeeded, from the counters between two polls. A counter that fell is read as a restart. It rides in the snapshot data as `slos`, and `.claude/slos.tsv` names it.
- Its snapshot never blocks on a series write that is still in flight.
