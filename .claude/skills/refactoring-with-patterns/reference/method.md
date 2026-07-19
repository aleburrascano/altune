# Feature survey — any language

Three questions, answered before reading any file contents: **where is the
feature's boundary, what crosses it, and is the boundary enforced.** The method
is identical in every language; only the tooling changes. Everything here has a
universal floor that needs nothing but `rg` (ripgrep) plus `sort`/`uniq`/`wc` —
enough to produce the headline boundary verdict anywhere. Two steps (cycles,
lint) have a precision upgrade when the ecosystem ships a tool you can invoke
correctly; without it, say so in the report rather than claiming a clean result.

Placeholders used below — fill them from step 0:
- `<terms>` — the feature's name and its near-synonyms, `rg`-alternated: `foo|bar`
- `<ext>` — the source-file glob for this ecosystem: `*.rs`, `*.{ts,tsx}`, `*.py`
- `<dir>` — the directory or module name the feature lives in

## 0. Identify the ecosystem

The repo root carries a build/manifest file that names the language and its
tooling, and the source tree follows that ecosystem's conventional layout — you
recognize these on sight. Read the root manifest and the top-level directory
names and infer three things: the **language**, the **source extensions** to
survey (`<ext>`), and which **graph/lint tools** (steps 3–4) exist for it.

This is recognition, not a lookup table: any manifest that pins a language counts
— `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `mix.exs`, `pom.xml`,
`Gemfile`, `composer.json`, `*.csproj`, a `Makefile` naming a compiler — whatever
the ecosystem uses. If none is recognizable, pick the dominant source extension
and proceed; the floor below still works.

## 1. Locate the boundary — universal floor

The one question that produces F1: **how many directories hold this feature's
code?**

```bash
rg -l -i '<terms>' -g '<ext>' -g '!*test*' \
  | xargs -n1 dirname | sort | uniq -c | sort -rn
```

Read the count as the verdict:

- **1–2 directories** → it has a boundary. Audit inside it.
- **3–4** → weak boundary. Worth a finding, but the audit proceeds.
- **5+**, or a directory named for something else holding the feature's code →
  no boundary. **That's F1.** The fix is structural — create the directory/
  module and move the code — and every pattern finding is downstream of it.

If a directory is already named for the feature, start there.

Layout smear to watch for: the feature's code spread across layer-named
directories at once — `controllers/`+`services/`+`models/`, or
`components/`+`hooks/`+`store/`+`api/` — is layer-first layout, a smear even when
each directory looks tidy.

## 2. What crosses the boundary — universal floor

**Fan-out** (what the feature imports) — read the import/require/use lines at the
top of the feature's files:

```bash
rg -i '^\s*(import|from|require|use|include|using)\b' <dir> | sort -u
```

**Fan-in** (what imports the feature):

```bash
rg -l -i '<dir>' -g '!*test*' | xargs -n1 dirname | sort -u
```

- A feature imported by **another feature** is a boundary violation.
- A feature importing the **transport/UI layer** is inverted direction.

Both are findings, both structural.

**Shared-module rot** — a `common/` / `utils/` / `shared/` directory imported by
everything welds every feature to every other, so no feature can be extracted:

```bash
# how big is the suspected god-module, and does it have one subject?
rg --files <shared-dir> -g '<ext>' -g '!*test*' | xargs wc -l | sort -rn
```

Splitting it along feature lines is usually worth more than any pattern.

**File sizes** as a rough "doing too much" proxy:

```bash
rg --files -g '<ext>' -g '!*test*' | xargs wc -l | sort -rn | head -20
```

## 3. Cycles — precision, needs a graph tool

A cycle (A→B→A, or any longer loop) is a real problem needing no judgment call —
**lead with any you find.** Textual grep cannot resolve aliased imports,
re-exports, or dynamic imports, so it can't find cycles reliably. Use the
ecosystem's graph tool if you can run it correctly — among others:

- **JS/TS** → `npx --yes madge --circular --extensions ts,tsx src/`
- **Go** → none needed — the compiler forbids cycles, so a passing build = none
- **Python** → `pycycle` / `pydeps`
- **Rust** → `cargo-modules` (the compiler catches many within a crate)

If unsure of a tool's exact flags, confirm before running rather than guessing.
**If no graph tool is available, state in the Boundary section "cycles not
checked — no graph tool" — do not claim there are none.**

## 4. Enforce the direction — precision, needs the linter

When the audit finds a direction or cross-feature violation, propose a rule that
**fails CI** — a config that fails CI beats discipline and stops the boundary
eroding on the next feature. Prefer extending config the project already has over
adding a tool. Use whatever import-boundary linter the ecosystem provides — among
others:

- **JS/TS** → dependency-cruiser forbidden zones, or eslint
  `import/no-restricted-paths`
- **Go** → go-arch-lint component graph, or depguard rules

Propose the rule alongside the fix. If the project has no linter that can express
it, recommend one rather than hand-writing a rule for a tool that isn't there.
