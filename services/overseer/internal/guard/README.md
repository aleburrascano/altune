`guard` holds the overseer's whole-tree contract and architecture tests.

- Its tests check that the wire JSON matches the fixtures and the web types, that every bucket is registered, writes its declared key series and grades its own snapshot, and that a source-down snapshot carries a classified reason.
- They check the credential behaviour end to end: a spent refresh token signs in again, a failed credential never marks go-api down, and a blue-green flip is read without a restart.
- They keep `core` and the buckets JSON-only, and keep buckets from building their own static token source.
- `AdminPathLiterals` finds literal admin paths in production code, and `TestNoAdminPaths` fails when one appears.
- Its tests may import any module, and no other package imports it.
