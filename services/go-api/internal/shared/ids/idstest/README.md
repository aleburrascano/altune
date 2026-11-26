`idstest` builds the `ids.Source` tests hand to code that mints ids. `Sequence` counts up from `00000000-0000-0000-0000-000000000001`, one UUID per draw, and each call to `Sequence` starts over. A source is safe to draw from many goroutines.

Production code never imports this package.
