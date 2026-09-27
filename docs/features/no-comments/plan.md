# No comments, anywhere

## Outcome, what the person gets: the problem, who has it, what done looks like to them

The operator never sees a `//` or `/* */` in altune's code again: frontend, backend, tests, scripts. Today about 17,000 comment lines remain, even though the constitution says "No comments", and new ones keep landing. Both "no new comments" gates skip test files, and nothing checks overseer at all, so recent PRs (#2970, #2974, #2982, #2994, #2995) added comments without being stopped.

Done means:
- Zero comment lines remain.
- The 72 lint and compiler suppressions are gone, each fixed at the code that needed it.
- Whole-file checks in CI block any comment in any file, whoever writes it.

The only survivors are the 4 `//go:embed` lines, which Go has no other syntax for; see Decisions.

## Appetite, the declared budget: "<n> tickets, <n> waves" (Shape Up, Ryan Singer). A budget to check against, never a cut: ticketize going over it checks in with the user

28 tickets, 4 waves.

## Scope

### In, every piece the outcome needs, across all layers

**Stop the bleed (tracer).** Widen today's diff-scoped gates now, before any deletion:
- `apps/mobile/scripts/lint-changed-lines.mjs` also covers test files and everything under `apps/mobile/`, not just `src/`.
- `services/go-api/scripts/lint-changed-comments.go` also covers `_test.go`.
- The same Go check runs for `services/overseer`, plus a TS check for `services/overseer/web`.
- A push with no usable diff base fails closed; today it skips.

**Removal tools**, each proven to change nothing but comments:
- **TS/JS**: a script built on the TypeScript scanner that strips every comment, then runs the repo's prettier. Test: every file's AST, with comments dropped, is identical before and after.
- **Go**: a script built on `go/parser` + `go/format` that drops every comment group except directives (`//go:`, `//nolint`) until their own tickets remove them. Test: identical AST, plus `go build ./... && go vet ./...` pass.
- **Shell**: `services/go-api/scripts/nocomments`, a `kind` registry (`main.go`, `kind.go`) with shell as its first kind (`shell.go`, on `mvdan.cc/sh/v3/syntax`), `strip`/`check`/`diff` modes. Only the line-1 `#!` shebang survives; everything else the parser calls a comment goes, including `# shellcheck disable=...` lines. Test: comment-free prints of the original and the stripped source are byte-identical, over a fixture and over every tracked shell script.
- **Every other kind** (YAML, Dockerfile, Caddyfile, line-config and TOML, SQL, CSS, root JS, Markdown and HTML comments) plugs into the same `nocomments` `kind` registry, one file kind per ticket, proven the same way: parse, delete comment spans, reprint comment-free, require the before and after to match.

**Deletion, one `mechanical` ticket per module** (comment lines on main in brackets):
- **Mobile (5 tickets):**
  - `src/shared` [1,315]
  - `features/playback` [672]
  - `features/detail` [550]
  - `features/auth` [546]
  - `features/library`, `discover`, `settings`, `_template`, `src/app`, `__tests__`, `jest`, `scripts` [~670]
- **go-api (7 tickets):**
  - `internal/discovery` [1,967]
  - `internal/catalog` [1,927]
  - `internal/acquisition` [1,084]
  - `internal/app` [1,004]
  - `internal/playback` [1,002]
  - `internal/shared` + `observe` + `cmd` + `scripts` [~1,050]
  - `internal/auth` + `feedback` [973]
- **overseer (2 tickets):**
  - `internal` [3,996]
  - `web/src` + `cmd` [212]

**Suppressions, fixed at the cause.** Each tells a real story:
- **25 `//nolint:nilerr`** (go-api discovery providers, cache, streamrip). Artwork and enrichment calls turn an error into `("", nil)`, so the failure is invisible to the caller and to telemetry. Fix: return the error, or a named `ErrArtworkUnavailable`, and let the caller degrade explicitly (and count it). Split in two tickets:
  - artwork providers: `coverartarchive`, `deezer_artwork`, `discogs`, `fanarttv`, `genius`, `itunes_artwork`
  - the rest: `discogs` enrichment, `identity_store_cache`, `streamrip/source.go`
- **4 `//nolint:gocritic`** (`deezer.go`, `musicbrainz.go`): the search DSLs need literal quotes. Fix: a named `dslQuote(s)` helper and no `Sprintf("\"%s\"")`.
- **6 `//nolint:errcheck`** (overseer `goapi` test stubs): fix with `_, _ = w.Write(...)`, an explicit discard.
- **15 `// @ts-expect-error`** (mobile tests). These are compile-time tests that a branded id (`TrackId`, `PlaylistId`) or a union rejects bad input. The code isn't broken; the directive is the test. Fix: comment-free type assertions (an `Expect<Not<IsAssignable<string, TrackId>>>`-style helper in `jest/`) that fail `tsc` the same way.
- **2 `/// <reference types>`** (overseer web). Fix: `compilerOptions.types` in `tsconfig`, and `defineConfig` imported from `vitest/config`.
- **11 `//go:build integration`**. Fix: a `testenv.RequireIntegration(t)` helper that skips unless `INTEGRATION=1`, with CI setting the variable where it now passes `-tags integration`.
- **3 `//go:build unix` / `!unix`** (`execcmd/procgroup_*`, `filesystem_stall_unix_test.go`). Fix: filename constraints (`_linux.go`, `_darwin.go`, `_windows.go`), which need no comment.
- **2 `//go:build ignore`** (the `scripts/lint-changed-*.go` scripts). Fix: each moves into its own `scripts/<name>/main.go` directory; `precheck.sh` and `test-backend.yml` call the new paths.

**Deletion, shell and every other kind, one ticket per area** (comment lines on main in brackets):
- **Shell** [~420]: `.github/workflows/*.sh`, `scripts/*.sh`, `apps/mobile/e2e/agent-browser/*.sh`, `services/go-api/deploy/*.sh` + `duckdns`, `services/go-api/scripts/guardrails.sh`.
- **YAML** (29 files): `.github/workflows/*.yml` and every other tracked `.yaml`/`.yml`.
- **Dockerfile** (2 files): `services/go-api/deploy/Dockerfile`, `services/overseer/Dockerfile`; the mandatory `# syntax=` line-1 directive is the only survivor, same rule as the shell shebang.
- **Caddyfile** (1 file): `services/go-api/deploy/Caddyfile`.
- **Line-config and TOML** (4 files): `services/go-api/.air.toml`, `services/go-api/.env.example`, `services/go-api/deploy/.env.staging.example`, `services/go-api/deploy/caddy/staging-upstream.conf`.
- **SQL** (25 files): every tracked `.sql` migration.
- **CSS** (1 file): `services/overseer/web`'s stylesheet.
- **Root JS** (2 files): `commitlint.config.js`, `dangerfile.js`, folded into the existing TS/JS tool and ESLint rule below, since they are ordinary `.js` files outside `apps/mobile` and `services/overseer/web`'s current scope.
- **Markdown and HTML comments** (2 files): the `<!-- -->` syntax in `.github/PULL_REQUEST_TEMPLATE.md` and `services/overseer/web/index.html`. Prose text itself stays out of scope (see Out); only the HTML comment syntax goes.

**Close-out: whole-file bans.**
- **TS/JS**: a local ESLint rule (`eslint-rules/no-comments.js`, reporting every `sourceCode.getAllComments()`) at `error` on every file in `apps/mobile` and `services/overseer/web`, tests included, and now also the two root JS config files above. The comment check leaves `lint-changed-lines.mjs`.
- **Go**: the comments script becomes a whole-tree check over go-api and overseer, tests included, allowing only `//go:embed`. It is wired as a required CI step in both backend workflows and in `precheck.sh`.
- **Shell and every other kind above**: `nocomments check` (and each new kind's own check, once its ticket lands) becomes a whole-tree gate over every tracked file of its kind, allowing only the one kept directive named above (the shebang, `# syntax=`, or none). Wired into `precheck.sh` and the relevant CI workflow the same way as the Go check.

### Out, only different jobs, each with a one-line reason

- Markdown docs and READMEs: prose documents, not code; their embedded `<!-- -->` comment syntax is in Scope, the prose itself is not.
- `node_modules` and generated build output: not ours to edit.
- The workflow's own doctrine that asks for comments (`~/.claude/workflow/build/test-conventions.md` "the issue goes in a comment", `ARCHITECTURE.md` "with the issue in a comment"): fixed outside the repo in this chat; the issue link lives in the PR's `Closes #N`, which `git blame` reaches.

## Pre-mortem, three lines "It failed because <cause>", imagined after the fact (Gary Klein, HBR 2007), each answered in Risks

- It failed because the removal tool ate code that only looked like a comment: a `//` inside a string or URL, a JSX text node, a regex literal.
- It failed because the module-wide deletions collided with the platform-free epic's tickets in the same files, and both queues stalled on rebases.
- It failed because removing the suppressions changed behaviour: an artwork failure started failing a whole search, or integration tests quietly stopped running in CI.

## Risks, anything dangerous that stays in scope (migration, security, breaking dependents), stated plainly; each pre-mortem cause named with its answer

- **Tool eats code (pre-mortem 1).**
  - Both tools work on the language's own scanner or parser, never regex, so strings, URLs, JSX text and regex literals are tokens and not comments.
  - Each tool's test proves AST identity, comments aside, on the whole repo.
  - Every deletion ticket's Verify runs its module's full tests, typecheck and lint.
- **Collisions with the platform-free epic (pre-mortem 2).**
  - Each mobile deletion ticket touches every file in its module, so the five mobile deletion tickets go first. The platform epic's tickets for a feature depend on that feature's deletion ticket.
  - The deletions are fast, pure-delete PRs.
  - The expedite web-dialog bug still jumps both queues.
- **Suppression fixes change behaviour (pre-mortem 3).**
  - The `nilerr` tickets are behaviour-preserving for users, because search still returns results without artwork. The only new behaviour is that the failure becomes visible and counted. Pinned by must-hold 4. Labelled `risk`; discovery is tier 1.
  - The integration-tag ticket must show the integration tests running (not skipping) in CI. Pinned by must-hold 5.
- **Knowledge in deleted comments.** Some comments carry the reason for a security decision, such as auth's #655 and #1637 link-spending notes. Git keeps every deleted line (`git log -S "<text>"`), and each PR's `Closes #N` stays reachable through `git blame`. Where a reason guards a security decision, the deletion ticket checks that a test already pins it by name, and names any gap in its PR for a follow-up ticket; it does not re-add a comment.

## Build, where it lives, `extends <module>` or the decisions made with rejected alternatives; a mermaid diagram when the shape isn't obvious. When the feature has UI on more than one platform, its first line is `Platforms: <list>`; builder, review and qa key their platform checks on it

Extends the existing gates: `apps/mobile/scripts/lint-changed-lines.mjs`, `services/go-api/scripts/lint-changed-comments.go`, `scripts/precheck.sh`, `.github/workflows/test-mobile.yml` and `test-backend.yml`, and overseer's CI workflow. The TS tool uses the `typescript` and `prettier` packages the app already has, and the Go tool uses the standard library; the one new dependency is `mvdan.cc/sh/v3`, linked only into the `nocomments` script's binary, for the shell kind's parser.

Order: the tracer and the tools, then deletions and suppression fixes in parallel per module, then the whole-file bans last, once the tree is clean.

Decisions:
- **A local ESLint rule, not a plugin.** Rejected: an npm no-comments plugin, a dependency for about 15 lines of rule.
- **Deletion is mechanical, per module.** Rejected: rewriting code per comment ("fix the code that wanted it"). At 17,000 lines that's a year of tickets. The constitution's self-documenting rule is held from here on by the ban, and review asks for better names on touched code.
- **Suppressions get fixed, not deleted.** Deleting a suppression alone turns lint or `tsc` red. Each is a defect with a named fix above.

## First slice, the walking skeleton: how a person gets in, and the one thing they can do end to end

The operator opens a PR that adds `// note` to a mobile test file and to an overseer Go file, and both are blocked in CI. Before this epic, both were allowed. The tracer lands that, and the smallest deletion (`features/settings` and friends, in the fifth mobile ticket) proves the TS tool end to end.

## Must-holds, rules that must always hold (living, grows during the build), each one a list item:

1. No TS/JS file in `apps/mobile` or `services/overseer/web`, tests included, contains a comment. [repo-test: apps/mobile/eslint-rules/__tests__/no-comments.test.js]
   - Example: linting `const a = 1; // x` → one `no-comments` error; linting `const url = 'https://x.dev';` → none.
2. No Go file in go-api or overseer, tests included, contains a comment other than `//go:embed`. [repo-test: services/go-api/scripts/lintcomments/main_test.go]
   - Example: a file containing `// hello` → exit 1 naming file and line; a file whose only comment is `//go:embed clip.mp3` → exit 0.
3. The removal tools change nothing but comments. [repo-test: apps/mobile/scripts/__tests__/strip-comments.test.mjs]
   - Example: `const s = "// not a comment"; /* gone */ f();` → `const s = "// not a comment";\nf();`, AST otherwise identical.
4. An artwork provider failure still degrades, never fails the search, and is now counted. [repo-test: services/go-api/internal/discovery/adapters/providers/discogs_test.go]
   - Example: Discogs artwork endpoint answers 500 → the lookup returns `ErrArtworkUnavailable`, and the search result still returns with empty artwork and no error.
5. Integration tests still run in CI after the build tags go. [operator: gh run view <latest test-backend run> --log | grep -c -- '--- PASS: TestPgxSearchHistoryRepo']
   - Example: the latest `test-backend` run on main → count ≥ 1, with no `SKIP` for integration tests.
6. The type tests still reject bad input without a directive. [repo-test: apps/mobile/src/shared/offline/__tests__/pinnedFiles.test.ts]
   - Example: changing the helper's assertion to accept a raw `string` where a `TrackId` is required → `tsc --noEmit` fails.

## Expected signals, what the running thing should show once built: the API endpoints and pages that change (everything unlisted must stay the same), and how telemetry should move ("p95 of GET /orders stays under 200ms"); see `~/.claude/workflow/feedback-loop.md`

- No endpoint or page changes; `uicheck` and `replay` show no difference.
- Telemetry: after the `nilerr` fix, artwork-provider failures appear as a counted degrade instead of silence. Expect a small, non-zero rate where there was none before; search p95 unchanged.

## Decisions, what the user answered or accepted as a default in step 3

- Scope is every tracked kind: TS/JS and Go (`//` and `/* */`), and shell, YAML, Dockerfile, Caddyfile, line-config, TOML, SQL, CSS, root JS, Markdown and HTML (`#`, `--`, and `<!-- -->` alike), tests included (operator: yes, 2026-09-26, "literally ban // and #").
- Doc comments go too, Go doc comments and JSDoc included (operator: yes).
- Every suppression is looked into and fixed at the cause, not deleted (operator: yes).
- Default: the 4 `//go:embed` lines stay as the ban's only exception. Go has no other syntax for embedding, and the alternative (reading files at runtime) would mean shipping `clip.mp3`, the eval goldens and overseer's web bundle beside the binaries in every image. The operator can overrule this.
- Default: the mobile deletion tickets run before the platform-free epic's tickets for the same feature.
