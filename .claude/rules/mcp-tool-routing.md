# MCP tool discovery — required before routing

`serena`, `codebase-memory-mcp`, `context7`, and the `software-architecture-design` vault are **deferred tools** in this session: calling them directly fails with `InputValidationError` until their schema is loaded. `~/.claude/rules/tool-routing.md` says which of them to use for code navigation, graph queries, and library docs — but naming the right tool isn't enough if it's never loaded.

Before following that routing table, call `ToolSearch` first — e.g. `ToolSearch({query: "select:mcp__serena__find_symbol"})` — then call the tool. Do this proactively for any task the routing table covers; don't default to `Grep`/`Read`/training-data recall just because the MCP tool isn't loaded yet in this turn.
