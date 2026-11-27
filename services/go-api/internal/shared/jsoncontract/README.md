`jsoncontract` owns how a JSON contract fixture is compared with a live response and updated. This package registers the `-update-contract` test flag, so a test binary that imports it must not declare that flag itself.

Two kinds of fixture use it:

- Local fixtures, `AssertMatchesFixture(t, path, body)`: the response must keep every key and JSON kind the fixture has. Extra keys in the response pass. The monitoring handlers use these next to their tests.
- Wire fixtures, in `contracts/wire/` at the repo root, which the client reads. `AssertWire(t, name, body)` fails on a missing key, a changed kind and an unexpected key, so any change to the wire shape fails the Go test until the fixture is updated. `AssertWireExact(t, name, body)` compares the whole content, for a list the client must match value for value (error codes, event types, routes, stages, statuses). Both take the fixture name without `.json`; `WirePath(name)` is where it lives.

A wire fixture is written by the real handler or serializer, through the router or a handler test over the memory adapters, never by hand. The client then decodes the same file with the decoder of that endpoint in a jest test named `*Contract.test.ts` or `*.contract.test.ts` (see `client/jest/wireFixtures.ts`), so the Go test fails when the Go shape moves and the jest test fails when the client cannot decode it.

Updating a fixture after an intended change:

```
scripts/update-wire-contracts.sh
git diff contracts/wire
```

The script reruns the `TestWire_*` tests of every package that imports `jsoncontract` with `-update-contract`, which rewrites the fixtures from the live responses. Review the diff, then run the client contract tests (`npx jest '(\.contract|Contract)\.test\.tsx?$'` from `client/`). A normal test run, in the precheck and in CI, never rewrites a fixture: it fails on drift and names the key.
