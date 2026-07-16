# TypeScript survey commands

## Contents

- Locating the feature boundary (do this first)
- Cycles
- Import graph at the boundary
- Enforcing import direction

`npx --yes` downloads on first run and needs network. If offline, fall back to
grep and file-size survey and say the graph tools were unavailable — don't
silently skip the step.

## Locating the feature boundary

The question: **how many directories contain this feature's code?**

```bash
ls src/features 2>/dev/null || ls src
```

If there's a directory named for the feature, start there. If not, find where it
actually lives:

```bash
grep -ril 'playback\|player\|nowPlaying' --include='*.ts' --include='*.tsx' src/ \
  | grep -v '.test.' | xargs -n1 dirname | sort | uniq -c | sort -rn
```

Read the count as the verdict:

- **1-2 directories** → it has a boundary. Audit inside it.
- **3-4** → weak boundary. Worth a finding, but audit proceeds.
- **5+** → no boundary. That's F1. The fix is structural — create the directory
  and move the code — and every pattern finding is downstream of it.

Frontend-specific smear to look for: feature logic living in `components/`,
`hooks/`, `store/`, and `api/` simultaneously. That's layer-first layout, and it
means a playback change touches four directories every time.

## Cycles

Unlike Go, TypeScript permits import cycles. They compile, they run, and they
cause undefined-at-import-time bugs that look like anything but a cycle. Check
before anything else:

```bash
npx --yes madge --circular --extensions ts,tsx src/
```

Every cycle reported is a real problem needing no judgment call. Lead with them.

## Import graph at the boundary

```bash
# what the feature depends on
npx --yes madge --extensions ts,tsx --depends src/features/playback src/

# repo-wide summary: orphans, leaves, depth
npx --yes madge --extensions ts,tsx --summary src/

# full graph as JSON, for finding what imports the feature
npx --yes madge --extensions ts,tsx --json src/ > /tmp/graph.json
```

A feature imported by another feature is a boundary violation. A feature importing
from `components/` or `pages/` is inverted direction. Both are findings, both are
structural.

`dependency-cruiser` gives more, at the cost of a config file:

```bash
npx --yes depcruise --init
npx --yes depcruise src --output-type archi
```

File sizes, as a rough proxy for doing too much:

```bash
find src -name '*.ts' -o -name '*.tsx' | grep -v '.test.' \
  | xargs wc -l | sort -rn | head -20
```

Barrel files (`index.ts` re-exporting everything) are worth flagging: they defeat
tree-shaking, hide the real dependency graph, and are a common cycle source.

```bash
find src -name 'index.ts' -exec grep -l 'export \*' {} +
```

## Enforcing import direction

The structural fix that outranks any pattern — it stops the boundary from eroding
again on the next feature. Check what exists first (`.dependency-cruiser.js`,
`eslint.config.js`, `.eslintrc*`) and prefer extending it over adding a tool.

**dependency-cruiser** — rules fail CI:

```javascript
// .dependency-cruiser.js
module.exports = {
  forbidden: [
    {
      name: 'no-circular',
      severity: 'error',
      from: {},
      to: { circular: true },
    },
    {
      name: 'no-cross-feature',
      comment: 'Features talk through shared, not to each other.',
      severity: 'error',
      from: { path: '^src/features/([^/]+)/.+' },
      to: { path: '^src/features/(?!$1)[^/]+/.+' },
    },
    {
      name: 'domain-imports-no-ui',
      comment: 'Feature logic must not depend on presentation.',
      severity: 'error',
      from: { path: '^src/features/[^/]+/(model|api)' },
      to: { path: '^src/(components|pages)' },
    },
  ],
};
```

```bash
npx --yes depcruise src --config .dependency-cruiser.js
```

**ESLint** — zero new tooling if it's already configured:

```javascript
// eslint.config.js
'import/no-restricted-paths': ['error', {
  zones: [{
    target: './src/features/playback',
    from: './src/features/library',
    message: 'Features must not import each other.',
  }],
}],
```
