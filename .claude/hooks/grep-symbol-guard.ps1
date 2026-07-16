$ErrorActionPreference = 'Stop'

$inputJson = [Console]::In.ReadToEnd()
try {
    $data = $inputJson | ConvertFrom-Json
} catch {
    exit 0
}

$pattern = $data.tool_input.pattern
if (-not $pattern) { exit 0 }

# Must be a single bare identifier: no spaces, no regex metachars beyond word chars/underscore.
if ($pattern -notmatch '^[A-Za-z_][A-Za-z0-9_]*$') { exit 0 }

# Must look like a code identifier: snake_case (has underscore) or camelCase/PascalCase (uppercase after position 0).
$hasUnderscore = $pattern -match '_'
$hasInnerUpper = $pattern.Length -gt 1 -and ($pattern.Substring(1) -cmatch '[A-Z]')
if (-not ($hasUnderscore -or $hasInnerUpper)) { exit 0 }

# Must target source code specifically, not be a generic/absent search target.
$target = $null
if ($data.tool_input.glob) { $target = [string]$data.tool_input.glob }
elseif ($data.tool_input.type) { $target = [string]$data.tool_input.type }
elseif ($data.tool_input.path) { $target = [string]$data.tool_input.path }

if (-not $target) { exit 0 }

$sourceExtPattern = '\.(go|ts|tsx|js|jsx|py)($|["''\s,\}\]])|(^|[\s,\[\{])(go|ts|tsx|js|jsx|py|typescript|javascript|python)($|[\s,\]\}])'
if ($target -notmatch $sourceExtPattern) { exit 0 }

$reason = "'$pattern' looks like a code-symbol lookup, not a text search. Load an MCP tool first: ToolSearch({query: 'select:mcp__serena__find_symbol'}) for a single definition, or 'select:mcp__codebase-memory-mcp__search_graph' for broader/related-code questions. See .claude/rules/mcp-tool-routing.md."

$output = @{
    hookSpecificOutput = @{
        hookEventName            = "PreToolUse"
        permissionDecision       = "deny"
        permissionDecisionReason = $reason
    }
} | ConvertTo-Json -Depth 5 -Compress

Write-Output $output
exit 0
