# Software-architecture-design vault is the authoritative pattern reference

This project treats the `software-architecture-design` vault MCP as the canonical reference for: SOLID principles, DDD building blocks, architectural patterns (hexagonal, vertical slice, layered, microservices), the 23 GoF design patterns, testing strategies, resiliency patterns, consistency models, observability practices.

**When making design decisions, defining domain models, writing tests, or reviewing code: consult the vault first.** Cite the relevant vault note(s) when applying or recommending a pattern. This isn't optional ceremony — it's how this project's quality bar is preserved session over session.

## Search entry points

- `mcp__software-architecture-design__vk_search "<keyword>"` — BM25 full-text
- `mcp__software-architecture-design__vk_search_by_tag "<tag>"` — exact tag match (tags include: `solid`, `design-pattern`, `ddd`, `architectural-pattern`, `behavioral`, `structural`, `creational`, `gof`, `ui`, etc.)
- `mcp__software-architecture-design__vk_list_notes prefix: "wiki/topics/"` — synthesis / navigation pages
- `mcp__software-architecture-design__vk_list_notes prefix: "wiki/concepts/"` — individual patterns / principles
- `mcp__software-architecture-design__vk_get_note path: "wiki/concepts/<name>.md"` — full content

## When the vault returns nothing

State explicitly: `"vault returned no matches for <topic>"`. Do not silently proceed. Either:
- broaden the search (synonyms, related concepts)
- consult the Claude-Code vault (`mcp__claude-code__vk_search`) if the question is about *how to work*, not *how to design*
- proceed with `[INFERRED]` tag and flag for ADR review

## Cross-vault discipline

- `software-architecture-design` vault → **how to design well** (patterns, principles, architecture)
- `claude-code` vault → **how to use Claude well** (skills, hooks, context engineering, agent workflows)

Don't confuse them. A "Repository Pattern" question goes to the architecture vault. A "should this be a skill or a hook" question goes to the Claude-code vault.

## Citation format in output

When citing a vault note in a spec, plan, ADR, or review, use:

```
[vault: wiki/concepts/Repository Pattern.md]
```

Embedded in prose:
> The catalog persistence layer uses a Repository per Aggregate Root [vault: wiki/concepts/Repository Pattern.md], mapping the `Track` aggregate to a row-per-track schema.
