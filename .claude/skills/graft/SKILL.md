---
name: graft
description: Query the codebase through Graft's prebuilt knowledge graph instead of exploring files from scratch. Use to locate where a behavior lives, find who calls a symbol and its blast radius, orient in an unfamiliar area, or run an exhaustive regex sweep. Fast, local, no API key. Trigger words: graft, where is X handled, who calls X, blast radius of X, map the repo, find the code that does X.
---

# Graft

A prebuilt map of this codebase. Instead of reading files one by one to find something, ask the map. It returns exact `file:line` hits ranked by relevance.

The graph is a **local cache** in `graft/` (already git-ignored, like `node_modules`). It is regenerable — never commit it.

## When to reach for this first

Before grepping around or reading a pile of files to answer "where does X happen" or "what breaks if I change Y", ask Graft. It is faster and points you straight at the symbol. Then read the specific `file:line` it names.

## Commands

Run from the repo root. All of these are read-only and need **no API key**.

```bash
graft ask "<task or question>"        # ranked nodes + exact file:line for a goal
graft map                             # high-level orientation: clusters, hubs, hotspots
graft callers <symbol>                # who calls / references this symbol
graft callers <symbol> --direction out  # reverse: what this symbol calls
graft callers <symbol> -d 2           # transitive blast radius, depth 2
graft grep "<regex>"                  # exhaustive regex sweep, grouped by symbol
```

Useful flags:

- `graft ask "..." --json` — machine-readable, when you want to parse the result.
- `graft ask "..." --in apps/mobile/` — scope to one sub-project of the monorepo.
- `graft ask "..." --no-refresh` — skip the pre-query graph refresh (faster, may be stale).
- `graft grep "..." -i --fixed` — case-insensitive literal string, not a regex.

This repo has two worlds: `apps/mobile/` (Expo RN + TS) and `services/go-api/` (Go). Use `--in` to stay in one.

## Keeping the graph fresh

The structural graph is built from source. After large changes, rebuild:

```bash
graft build                # rebuild structural graph (no LLM, no key)
```

`graft ask` auto-refreshes by default, so day-to-day you rarely run `build` by hand.

## Optional: the deep layer (needs a key)

`graft build --deep` adds LLM-written summaries and concept nodes for richer `ask` results. It needs a provider key via env vars (`GRAFT_PROVIDER`, `GRAFT_API_KEY`, `GRAFT_MODEL`). Skip it unless the plain graph is not enough — the structural graph already answers most "where is X" questions.

## What this skill is not

- Not an MCP server. This is the CLI only, driven by hand.
- Not a code reader or reviewer. It locates code; you still open the `file:line` to judge it.
- Not a source of truth to commit. The `graft/` folder is a throwaway cache.
