Infrastructure: `random` owns how code reads randomness.

A decision that depends on chance takes a `random.Unit` from its composition root and calls it, instead of calling `math/rand` itself, so a test can say what the draw is. A `Unit` returns a float in `[0, 1)`; `random.Real` is the production source. A required source is a constructor parameter with no default, and a constructor refuses a nil one with an error that names it, the same rule as `clock.Func`.

Tests build a source with `randomtest.Sequence` or `randomtest.Fixed` (`random/randomtest`). Production code never imports `randomtest`. Sites that still draw from `math/rand` directly (the egress retry jitter default, `backoff.Policy.Delay`, the ranking experiment shuffle) move to a `Unit` as their owners touch them.
