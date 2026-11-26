`guard` holds the overseer's whole-tree contract and architecture tests.

- Its running tests check that the wire JSON matches the fixtures, that every bucket package is registered, writes its declared key series and grades its own snapshot, and that a source-down snapshot carries a classified reason.
- They keep `core` and the buckets JSON-only, keep buckets from building their own static token source, and fail when a literal admin path appears in production code (`AdminPathLiterals`).
- The credential and web-type must-holds in `mustholds_invariants_test.go` are skipped placeholders that wait on their tickets. They check nothing yet.
- Its tests may import any module, and no other package imports it.
