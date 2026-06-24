# HTML Report Format

Same mechanics as `/improve-codebase-architecture`'s [HTML-REPORT.md](../improve-codebase-architecture/HTML-REPORT.md) — one self-contained file in the OS temp dir, Tailwind + Mermaid via CDN, the `/codebase-design` glossary for every architectural noun, diagrams carrying the weight over prose. Reuse that scaffold, styling, and diagram patterns. This file specifies only what differs.

## 1. Whole-backend coupling graph (top of report)

Before any cards, one Mermaid `flowchart` of the inter-context dependency graph: nodes are bounded contexts, edges are imports between them. Style any **cycle** red — a cycle means neither context can be extracted. This is the bird's-eye answer to "what can be pulled out?" and has no equivalent in the sibling skill's report.

```html
<pre class="mermaid">
  flowchart LR
    discovery --> shared
    catalog --> shared
    acquisition --> catalog
    classDef cycle stroke:#dc2626,stroke-width:2px;
</pre>
```

## 2. Self-grilled, tiered cards

Each card is one finding, **self-grilled** — the design questions answered in the card, not deferred to a live interview. Fields:

- **Title** — names the move (e.g. "Collapse the five detail enrichers behind one facade").
- **Tier badge** (`Batch` = slate, `Skim` = amber, `Grill` = rose) + the **coupling/cohesion kind** from HEURISTICS.
- **Files** — monospaced.
- **Before / After diagram** — per the sibling's patterns (mass diagram for a shallow interface, call-graph collapse for a facade).
- **Problem · Shape** — one sentence each.
- **Blast radius** — the callers that ripple, with the reference search that verified them: _"verified: 3 callers (search_wiring.go:41, …)"_.
- **Surviving tests** — what still exercises the behaviour after the change.
- **Counter-argument** — one line, the brake (amber box).

No "which would you like to explore?" closing — triage walks all findings. End the report with the tier counts instead: _"14 batch · 4 skim · 3 grill"_.
