`randomtest` builds the `random.Unit` sources tests hand to code that draws chance. `Sequence` returns each given value in order and then repeats the last; `Fixed` always returns one value. Every value must be in `[0, 1)`, and a bad value or an empty sequence panics when the source is built. Sources are safe to draw from many goroutines.

Production code never imports this package.
