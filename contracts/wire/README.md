Contracts: JSON fixtures that go-api writes and the client decodes, so both sides agree on the wire shape.

The folder sits at the repo root because neither side owns it. A change to the Go response shape fails a Go test, and a change the client cannot decode fails a jest test, until the fixture is updated on purpose.

## Must not

- Hold a fixture written by hand. A fixture comes from the real handler or serializer, so it proves what the server sends. A hand-written one only proves what someone remembered. The one exception is `endpoint_limits.json`: no generator writes it, and each side only reads it.
- Be read by production code. Only tests read it.

## Regenerate

From anywhere inside the checkout:

```
scripts/update-wire-contracts.sh
git diff contracts/wire
```

The script runs `go test -count=1 -run 'TestWire_' -update-contract` in `services/go-api`, on every package whose tests import `internal/shared/jsoncontract`. The flag makes `jsoncontract` rewrite each fixture from the live response instead of comparing. It exits 1 if no package imports `jsoncontract`, and 2 outside a git checkout. A run with nothing to change leaves `git diff` empty.

A Go test must be named `TestWire_*` for the script to rewrite its fixture. After a change, review the diff, then run the client side (below). A normal test run never rewrites a fixture.

## Who reads it

Go side, through `jsoncontract.AssertWire` (every key and JSON kind must match, and an unexpected key fails) and `AssertWireExact` (the whole content must match, for lists such as error codes, routes and stages):

- the `*wire*contract_test.go` files under `services/go-api/internal`, one set per module that has a wire shape. List them with `git grep -l AssertWire services/go-api`.
- two tests that read one fixture directly with `os.ReadFile`, so `AssertWire` does not find them: `services/go-api/internal/search/internal/adapters/handler/endpoint_limits_contract_test.go` (`endpoint_limits.json`) and `TestEveryDeclaredEventTypeHasAWireFixture` in `services/go-api/internal/shared/events/event_type_test.go` (`events_types.json`).

Client side, through `wireFixture(name)` in `client/jest/wireFixtures.ts`, which walks up from its own folder (at most 12 levels) to find `contracts/wire`:

- the `*Contract.test.ts` and `*.contract.test.ts` files under `client/src`. List them with `git grep -l wireFixture client/src`. Run them from `client/`:

  ```
  npx jest '(\.contract|Contract)\.test\.tsx?$'
  ```

- `client/src/shared/apiClient/__tests__/endpointLimits.test.ts` also reads `endpoint_limits.json`. Its name does not match the pattern above, so only a full jest run covers it.

`scripts/precheck.sh` runs the contract pattern when `contracts/wire/` or go-api changes.
