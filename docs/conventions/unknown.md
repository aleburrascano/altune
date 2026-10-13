# Where `unknown` is allowed in client code

**Status: decided (#2291).** Enforcement and the sweep are #2364.

`unknown` says "I have not checked this value yet". It is right at a boundary, where a value
arrives unchecked and gets narrowed on the spot. Anywhere else the code knows the type,
so it should name it.

## Count

`client/src`, tests excluded (`__tests__/`, `*.test.*`), on gitea/main c315eea3b: 363
`unknown` keywords, counted on the TypeScript AST.

| Position | Uses |
|---|---|
| Caught error, or a parameter or field that carries one (`err`, `error`, `reason`, `cause`, `*Error`) | 124 |
| Decoder input in `shared/apiClient` | 101 |
| Decoder input elsewhere: a `parse*`/`as*`/`narrow*` function, or a `*Wire.ts` file | 25 |
| Type-guard parameter (`value: unknown): value is T`) | 8 |
| Generic constraint or default (`A extends unknown[]`) | 5 |
| Type argument (`Record<string, unknown>`, `Promise<unknown>`, `Mutation<unknown, …>`) | 46 |
| Other (`readonly unknown[]` query keys, `() => unknown`, `as unknown as`, document fields) | 54 |

## Rule (MC-11)

`unknown` is allowed only in these positions:

1. **A caught error.** The `catch` binding, or a parameter or property signature named
   `e`, `err`, `error`, `reason`, `cause`, `thrown`, `caught`, or ending in `Error`.
2. **A decoder input.** Anything in `src/shared/apiClient/**` (the wire boundary; #2275 and #2276
   make `apiFetch`/`apiSend` return `unknown`), anything in a `*Wire.ts` file, and anything inside a
   function named `decode*`, `parse*`, `as*`, `narrow*` or `is*` followed by a capital letter.
3. **A type-guard parameter,** and the target type of a predicate (`value is Record<string, unknown>`).
4. **A generic constraint or default** (`T extends unknown[]`).
5. **`Promise<unknown>`**, for a promise awaited for its timing only.
6. **`JSON.parse`'s result,** as `const x: unknown = JSON.parse(…)` or `JSON.parse(…) as unknown`.

Everything else names a type: `QueryKey` from TanStack instead of `readonly unknown[]`, `() => void`
instead of `() => unknown`, a named payload type instead of `Record<string, unknown>`. A double cast
(`as unknown as T`) outside a decoder is flagged too.

## Check

The `@typescript-eslint/no-unsafe-*` rules can't express this: they flag `any`, not `unknown`,
and no typed rule bans `unknown`. Plain `no-restricted-syntax` can, with no type information.
It follows the MC-n pattern in [mobile-consistency.md](mobile-consistency.md): a module at
`client/eslint/consistency/mc-11-unknown.js` with today's sites in `client/baselines/consistency.baseline`.

```js
{
  ignores: ['**/__tests__/**', 'src/shared/apiClient/**', 'src/**/*Wire.ts'],
  rules: {
    'no-restricted-syntax': ['error', {
      selector: `TSUnknownKeyword:not(
        CatchClause > Identifier > TSTypeAnnotation > TSUnknownKeyword,
        Identifier[name=${ERR}] > TSTypeAnnotation > TSUnknownKeyword,
        TSPropertySignature[key.name=${ERR}] > TSTypeAnnotation > TSUnknownKeyword,
        :function[returnType.typeAnnotation.type='TSTypePredicate'] > Identifier > TSTypeAnnotation > TSUnknownKeyword,
        TSTypePredicate TSUnknownKeyword,
        :function[id.name=${DECODER}] TSUnknownKeyword,
        VariableDeclarator[id.name=${DECODER}] TSUnknownKeyword,
        TSTypeParameter TSUnknownKeyword,
        TSTypeReference[typeName.name='Promise'] > TSTypeParameterInstantiation > TSUnknownKeyword,
        VariableDeclarator[init.callee.object.name='JSON'][init.callee.property.name='parse'] > Identifier > TSTypeAnnotation > TSUnknownKeyword,
        TSAsExpression[expression.callee.object.name='JSON'][expression.callee.property.name='parse'] > TSUnknownKeyword
      )`,
      message: 'Name the type: unknown is only for caught errors, decoder and type-guard inputs, generic constraints and Promise<unknown> (docs/conventions/unknown.md).',
    }],
  },
}
// ERR     = /^_?(e|err|error|reason|cause|thrown|caught)$|Error$/
// DECODER = /^(decode|parse|as|narrow|is)[A-Z]/
```

A prototype of this config, run with `eslint` against `client/src` (499 files, about 20s, no
type information), flags **85 sites in 39 files**: features 39, shared 46.

## Follow-up

The sweep and the MC-11 rule are #2364.
