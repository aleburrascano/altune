# MCP tool discovery — required before routing

`serena`, `codebase-memory-mcp`, and `context7` are **deferred tools** in this session: calling them directly fails with `InputValidationError` until their schema is loaded via `ToolSearch`.

TRIGGER — call `ToolSearch` (e.g. `ToolSearch({query: "select:mcp__serena__find_symbol"})`) and load the matching tool BEFORE reaching for `Grep`/`Read`/training-data recall, whenever the request is shaped like:

- "where is X defined/declared" → `mcp__serena__find_symbol` / `find_declaration`
- "who calls/uses X" → `mcp__serena__find_referencing_symbols`
- "what implements X" → `mcp__serena__find_implementations`
- "structure of this file" → `mcp__serena__get_symbols_overview`
- "type errors / diagnostics on X" → `mcp__serena__get_diagnostics_for_file`
- "rename X everywhere" → `mcp__serena__rename_symbol`
- "replace/rewrite this function" → `mcp__serena__replace_symbol_body`
- "how does feature/system X work", "debug X", "trace this bug", "what's related to X" → `mcp__codebase-memory-mcp__search_graph` / `trace_path` / `get_architecture` / `get_code_snippet`
- any library/framework/SDK/API/CLI question → `context7` (`resolve-library-id` then `query-docs`)
- any non-trivial design/pattern decision → pattern lexicon (no MCP: manifest index is in context via the nested `CLAUDE.md`; `Read` the full entry at `~/.claude/lexicon/site/{path}/index.html` when tradeoffs matter)

SKIP — use `Grep` directly, no `ToolSearch` needed — when the target is plain text, not a code symbol: string literals, comments, config values, error message text, log lines.
