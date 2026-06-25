# Interpreter — Behavioral

> GoF behavioral pattern. (refactoring.guru has no Interpreter page — it omits this one GoF behavioral pattern; summary below is from the canonical GoF definition, not a fetched source.)

**Intent.** Given a language, define a representation for its grammar plus an interpreter that uses the representation to evaluate sentences in that language.

## Problem
A recurring class of problems is best expressed as sentences in a small language (filter expressions, query DSLs, rule predicates). Re-parsing and evaluating each occurrence ad hoc is error-prone and unmaintainable as the grammar grows.

## Solution
Model each grammar rule as a node type with an `interpret(context)` operation; compose nodes into an abstract syntax tree representing a sentence, then evaluate by walking the tree. Each terminal/non-terminal rule is one node kind.

## In altune
**Go:** **N/A / YAGNI today.** No bespoke DSL exists in the codebase — discovery search uses structured queries (`SearchQuery` value object), not an interpreted expression language. If one were ever needed, the native shape is a small `Expr` interface with an `Eval(ctx) (T, error)` method and concrete node structs (And/Or/Equals…) composed into a tree — but for anything non-trivial, prefer a real parser/library over a hand-rolled interpreter (`go-design-patterns.md`: "a little recode > a big dependency" cuts both ways — don't build a language you don't need).
**RN/TS:** **N/A.** UI filtering uses typed predicates and discriminated unions, not an interpreted grammar. No client-side DSL is warranted.
<N/A — no DSL in altune; documented for completeness of the GoF behavioral set.>

## When to reach for it
- A simple, stable grammar that genuinely recurs (a filter/rule mini-language) and is small enough that a full parser-generator is overkill.

## When to skip it
Almost always here. No DSL exists; don't invent one. Grammars grow, and a tree-of-node-types interpreter degrades fast — reach for an established parsing library before hand-rolling. The clearest sign you don't need Interpreter: you don't have a language.

## Related
- Patterns: [[visitor]] (operations over the AST without bloating node types), [[iterator]] (traverse the tree), [[command]]
- Refactoring moves: `../../refactoring/organizing-data.md` (Replace Type Code with State/Strategy — node dispatch), `../../refactoring/simplifying-conditional-expressions.md`
- Project rules: `../../backend/go-design-patterns.md`
