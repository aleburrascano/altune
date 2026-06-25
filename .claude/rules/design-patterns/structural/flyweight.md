# Flyweight — Structural

> GoF structural pattern. Source: https://refactoring.guru/design-patterns/flyweight

**Intent.** Fit more objects in memory by sharing the immutable (intrinsic) state common to many instances, passing the unique (extrinsic) state as parameters.

## Problem
You must hold a very large number of similar objects, and each one duplicates heavy unchanging data (a sprite, a config, a shared table). The duplication exhausts RAM and tanks performance.

## Solution
Split each object's state: intrinsic (shared, immutable — stored once in a pooled flyweight) and extrinsic (per-instance — passed in at call time). Thousands of contexts then reference a handful of shared flyweights instead of each carrying a full copy.

## In altune
**Go:** Rare and usually unnecessary — be plain about this. Go's value semantics, slices of small structs, and the GC make the particle-system memory crisis Flyweight was built for uncommon at altune's scale (a personal/family library, not a render loop). The idiomatic equivalents when sharing *does* matter: intern repeated strings, share an immutable lookup table via a package-level `var`, or use `sync.Pool` for reusing short-lived buffers (a different concern — reuse, not shared intrinsic state). Reach for explicit Flyweight only with a measured RAM problem and a huge object count.
**RN/TS:** N/A as a deliberate pattern. The runtime equivalent — interning/memoizing shared immutable values so many components reference one object — is handled by `useMemo`/module-level constants, not a hand-rolled flyweight factory.

No verified instance, and none expected: this is an optimization of last resort (`go-design-patterns.md`: "don't abstract prematurely"; apply only with profiled evidence).

## When to reach for it
- A profiled, real RAM ceiling caused by a massive count of objects with large shared immutable state, and no simpler fix exists.

## When to skip it
- Almost always here. Default to plain values/slices; share immutable data with a package-level `var` or interning. Flyweight adds an indirection (factory + intrinsic/extrinsic split) that's pure cost without a measured memory problem (KISS/YAGNI).

## Related
- Patterns: [[composite]] (use shared flyweights as leaf nodes to shrink a large tree), [[facade]] (the opposite scale — one object over a whole subsystem, vs many tiny shared objects), [[singleton]] (both share instances, but flyweights are immutable and many)
- Refactoring moves: `../../refactoring/organizing-data.md` — Change Value to Reference (share one canonical instance instead of copies)
- Project rules: `../../backend/go-data-structures.md` (preallocation, copy semantics, `weak.Pointer` for caches), `../../backend/go-design-patterns.md`
