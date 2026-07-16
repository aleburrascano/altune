# Go survey commands

## Contents

- Locating the feature boundary (do this first)
- Import graph at the boundary
- Shared-package rot
- Enforcing import direction

Prefer `go list` throughout: it ships with the toolchain, needs no install, and
reports what the compiler actually sees — build tags, vendoring and all. Check
`command -v jq` before using the jq variants; fall back to the `-f` forms rather
than asking for an install.

## Locating the feature boundary

The question: **how many packages contain this feature's code?**

If there's a package named for it, start there:

```bash
MOD=$(go list -m)
go list ./... | grep -i playback
```

If nothing comes back, the feature has no home. Find where it actually lives:

```bash
# files mentioning the feature, by directory
grep -ril 'playback\|player\|stream' --include='*.go' . \
  | grep -v _test.go | xargs -n1 dirname | sort | uniq -c | sort -rn
```

Read the count as the verdict:

- **1-2 directories** → it has a boundary. Audit inside it.
- **3-4** → weak boundary. Worth a finding, but audit proceeds.
- **5+** → no boundary. That's F1. The fix is structural — create the package and
  move the code — and every pattern finding is downstream of it.

## Import graph at the boundary

Once the boundary is known, what crosses it:

```bash
MOD=$(go list -m)
PKG="$MOD/internal/playback"

# what the feature imports (should point outward-in: stdlib, then inward deps)
go list -f '{{range .Imports}}{{.}}
{{end}}' "$PKG/..." | sort -u | grep "^$MOD"

# what imports the feature (should be main, handlers - not other features)
go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... \
  | grep "$PKG" | cut -d' ' -f1
```

A feature imported by another feature is a boundary violation. A feature importing
transport types is inverted direction. Both are findings, both are structural.

Fan-in across the repo, to spot the packages everything depends on:

```bash
MOD=$(go list -m)
go list -f '{{range .Imports}}{{.}}
{{end}}' ./... | grep "^$MOD" | sort | uniq -c | sort -rn | head -20
```

With jq, the whole edge list:

```bash
go list -json ./... | jq -r '.ImportPath as $p | .Imports[]? | "\($p) -> \(.)"' \
  | grep "$(go list -m)"
```

## Shared-package rot

Go forbids import cycles at compile time, so if the build passes there are none.
The cycle-shaped problem here is different: a `common`/`utils`/`shared` package
that everything imports, which welds every feature to every other.

```bash
# is there a package everything depends on?
MOD=$(go list -m)
go list -f '{{range .Imports}}{{.}}
{{end}}' ./... | grep "^$MOD" | sort | uniq -c | sort -rn | head -5

# how big is it, and does it have a single subject?
find . -path './internal/common/*' -name '*.go' -not -name '*_test.go' \
  -exec wc -l {} + | sort -rn
```

If the feature imports `common/` and `common/` is imported by six other features,
the feature cannot be extracted. Splitting `common/` along feature lines is
usually worth more than any pattern.

File sizes, as a rough proxy for doing too much:

```bash
find . -name '*.go' -not -name '*_test.go' -exec wc -l {} + | sort -rn | head -20
```

## Enforcing import direction

The structural fix that outranks any pattern — it stops the boundary from eroding
again on the next feature. Recommend `go-arch-lint` unless golangci-lint is
already configured, in which case use depguard and add no new tooling.

Check first: `command -v go-arch-lint`, `command -v golangci-lint`. If absent,
give the install command rather than assuming.

**go-arch-lint** — declarative component graph, fails CI:

```bash
go install github.com/fe3dback/go-arch-lint@latest
```

```yaml
# .go-arch-lint.yml
version: 3
workdir: .
components:
  handlers: { in: internal/handlers/** }
  playback: { in: internal/playback/** }
  library:  { in: internal/library/** }
  storage:  { in: internal/storage/** }
deps:
  handlers: { mayDependOn: [playback, library] }
  playback: { mayDependOn: [storage] }   # not library. features don't call features.
  library:  { mayDependOn: [storage] }
  storage:  { mayDependOn: [] }
```

**depguard** — zero new tooling if golangci-lint is present:

```yaml
# .golangci.yml
linters-settings:
  depguard:
    rules:
      playback:
        files: ["**/internal/playback/**"]
        deny:
          - pkg: "myapp/internal/handlers"
            desc: "features must not depend on transport"
          - pkg: "myapp/internal/library"
            desc: "features talk through storage, not to each other"
```
