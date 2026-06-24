# Refactoring catalog — altune

Martin Fowler's refactoring catalog (via [refactoring.guru](https://refactoring.guru/refactoring/techniques)), adapted to altune's stack: Go (hexagonal, no inheritance) and React Native + TypeScript (function components + hooks, vertical slices). Each technique carries its **smell**, the **move**, how it **applies in altune** (with verified file references where a real instance exists), and **when to skip it**.

These are **on-demand reference docs** — deliberately no `paths:` frontmatter, so they carry zero standing token cost. The structural-review skills consult them by name; they don't auto-load on every edit.

## How the review skills use this

The point is to flip the default from *recall-if-prompted* to *apply-by-default*. When a review skill finds a smell, it must **name the specific technique from this catalog and apply that move** — turning "this is shallow" into "this is *Inline Class* — here's the move." A finding that names a Fowler technique is grounded; a finding that doesn't is a vibe.

Consumed by:
- `/tighten-backend` — Go structural review
- `/tighten-frontend` — RN/TS structural review
- `/improve-codebase-architecture` — agnostic deepening review

## The six groups

| Group | Attacks |
|-------|---------|
| [Composing Methods](composing-methods.md) | Long functions, tangled locals — extract small named deep helpers |
| [Moving Features Between Objects](moving-features-between-objects.md) | Feature envy, wrong locality, pass-through layers |
| [Organizing Data](organizing-data.md) | Primitive obsession, leaky fields, type codes, magic numbers |
| [Simplifying Conditional Expressions](simplifying-conditional-expressions.md) | Nested `if`s, control flags, duplicated branches, type switches |
| [Simplifying Method Calls](simplifying-method-calls.md) | Lying names, leaky params, query/command tangles, error channels |
| [Dealing with Generalization](dealing-with-generalization.md) | Sibling duplication, fat/thin interfaces, abstraction in the wrong place |

## Stack caveat

Fowler's catalog is class/inheritance-centric. altune has neither classes (Go) nor class components (RN). Inheritance-based techniques — *Replace Type Code with Subclasses*, *Pull Up/Push Down*, *Extract Super/Subclass*, *Collapse Hierarchy*, *Replace Inheritance with Delegation* — are remapped to their compositional equivalents (struct embedding, interface satisfaction, function values, custom hooks) or marked **N/A** with the reason. The honest remapping is the value: don't force a class-hierarchy move onto a language without classes.
