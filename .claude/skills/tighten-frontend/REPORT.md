# HTML Report Format

Same mechanics as `/tighten-backend`'s [REPORT.md](../tighten-backend/REPORT.md) — which itself reuses `/improve-codebase-architecture`'s HTML scaffold: one self-contained file in the OS temp dir, Tailwind + Mermaid via CDN, the `/codebase-design` glossary for every architectural noun, diagrams carrying the weight over prose. This file specifies only what differs for the frontend.

**Filename — never overwrite.** Resolve the temp dir from `$TMPDIR` / `%TEMP%`. Write to `<tmpdir>/tighten-frontend-<YYYY-MM-DD>-<HHMMSS>-EST.html` (`TZ=America/New_York date +%Y-%m-%d-%H%M%S`) so every run is kept and runs are comparable. Tell the user the absolute path and open it.

## 1. Inter-feature coupling graph (top of report)

Before any cards, one Mermaid `flowchart` built from `fallow`'s boundary + cycle output: nodes are features (plus `shared/`), edges are imports between them. Style **cross-feature edges and cycles red** — each is a vertical-slice violation and means the feature can't be lifted cleanly. `shared/` should read as a sink: everyone points into it, it points nowhere out.

```html
<pre class="mermaid">
  flowchart LR
    discover --> shared
    library --> shared
    detail --> shared
    discover -.->|cross-feature| library
    classDef bad stroke:#dc2626,stroke-width:2px;
    class library bad
</pre>
```

## 2. Self-grilled, tiered cards

Each card is one finding, **self-grilled** — the design questions answered in the card, not deferred to a live interview. Fields mirror backend, with frontend specifics:

- **Title** — names the move (e.g. "Split DiscoverScreen: lift fetching into a useDiscover hook").
- **Tier badge** (`Batch` = slate, `Skim` = amber, `Grill` = rose) + the **coupling/cohesion kind** from HEURISTICS.
- **Files** — monospaced.
- **Prop interface, before / after** — the component's prop list shrinking is the centerpiece (mass diagram for a sprawling interface; call-graph collapse for a god component split into hook + presentational shell).
- **Blast radius** — who renders this component / calls this hook, verified via `fallow dead-code --trace` (exports) or reference search.
- **Surviving tests** — what still exercises the behaviour after the change.
- **Counter-argument** — one line, the brake (amber box): is this premature `shared/` extraction? is the prop drilling cheaper than context?

No "which would you like to explore?" closing — triage walks all findings. End with the tier counts (e.g. _"18 batch · 5 skim · 4 grill"_).
