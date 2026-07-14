# Template Method — Behavioral

> GoF behavioral pattern. Source: https://refactoring.guru/design-patterns/template-method

**Intent.** Define the skeleton of an algorithm in one place, deferring specific steps to be supplied per variant.

## Problem
Several variants run nearly the same algorithm — same overall sequence, a few differing steps — with the shared scaffolding copy-pasted across each. Duplication drifts; a fix to the skeleton must be repeated everywhere.

## Solution
Fix the step sequence once; let each variant supply only the steps that differ. The skeleton stays single-sourced; variation is confined to the pluggable steps.

## In altune
**Go:** No abstract base class — the GoF inheritance form is **N/A**. The compositional equivalent: a function owning the skeleton that takes the varying steps as **function values** — `fetch(ctx, url, parse func([]byte) (T, error))` keeps the HTTP request/timeout/error-wrapping skeleton in one place while each provider supplies its `parse`. This is the same seam as [[strategy]] viewed as "fixed flow, pluggable step." Provider adapters that share a scatter-gather/decode skeleton but differ in endpoint + parsing are this pattern.
**RN/TS:** A **custom hook owning the flow** that accepts callbacks/render-props for the variable parts, or `children`/render-prop composition. A hook that runs `loading → fetch → transform → expose` and takes a per-feature `transform` is Template Method; the component supplies the variable render.
<Conceptual — `fetch(ctx, parse func(...))` is the endorsed Go shape; confirm a concrete provider helper before citing a file.>

## When to reach for it
- 2+ variants share an algorithm's skeleton and differ only in a few steps.
- You want the sequence single-sourced and the variation localized to function values.

## When to skip it
Only one step varies across two callers — two near-copies can read clearer than a callback-threaded skeleton (Rule of Three). If the *structure* itself varies between variants, this isn't the fit — use [[strategy]] for wholly independent algorithms.

## Related
- Patterns: [[strategy]] (composition alternative — swap the whole algorithm vs. pluggable steps), [[command]]
- Refactoring moves: `../../refactoring/dealing-with-generalization.md` (Form Template Method; Pull Up Method), `../../refactoring/composing-methods.md` (Extract Method)
- Project rules: `../../../.claude/rules/backend/go-design-patterns.md`, `../../../.claude/rules/backend/go-structs-interfaces.md`
