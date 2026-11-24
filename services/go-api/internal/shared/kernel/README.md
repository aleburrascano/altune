`kernel` owns the identifiers, failure codes and track lengths every context shares.

## Track length

`TrackLength` decides what a plausible track length is, and when two lengths are the same recording.
- A plausible length is a finite number of seconds from 0 to 7 days. 0 means the length is unknown. `TrackLengthOf` refuses anything else with `ErrImplausibleTrackLength`.
- Two known lengths are the same recording when they differ by at most 5 s or 3% of the expected length, whichever is larger. `Allowance` gives that tolerance, and an unknown length is never the same recording as anything.
- Wire formats and database columns keep plain seconds. Adapters convert at the edge.
