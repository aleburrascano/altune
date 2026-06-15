---
paths: ["services/go-api/**/*.go"]
---

# Go conventions: naming, code style, error handling

---

## Part 1 — Naming

Go favors short, readable names. Capitalization controls visibility — uppercase is exported, lowercase is unexported. All identifiers MUST use MixedCaps, NEVER underscores.

> "Clear is better than clever." — Go Proverbs
>
> "Design the architecture, name the components, document the details." — Go Proverbs

To ignore a rule, just add a comment to the code.

### Quick Reference

| Element | Convention | Example |
| --- | --- | --- |
| Package | lowercase, single word, \_test suffix OK for test files | `json`, `http`, `tabwriter`, `http_test` |
| File | lowercase, underscores OK | `user_handler.go` |
| Exported name | UpperCamelCase | `ReadAll`, `HTTPClient` |
| Unexported | lowerCamelCase | `parseToken`, `userCount` |
| Interface | method name + `-er` | `Reader`, `Closer`, `Stringer` |
| Struct | MixedCaps noun | `Request`, `FileHeader` |
| Constant | MixedCaps (not ALL_CAPS) | `MaxRetries`, `defaultTimeout` |
| Receiver | 1-2 letter abbreviation | `func (s *Server)`, `func (b *Buffer)` |
| Error variable | `Err` prefix | `ErrNotFound`, `ErrTimeout` |
| Error type | `Error` suffix | `PathError`, `SyntaxError` |
| Constructor | `New` (single type) or `NewTypeName` (multi-type) | `ring.New`, `http.NewRequest` |
| Boolean field | `is`, `has`, `can` prefix on **fields** and methods | `isReady`, `IsConnected()` |
| Test function | `Test` + function name | `TestParseToken` |
| Acronym | all caps or all lower | `URL`, `HTTPServer`, `xmlParser` |
| Variant: context | `WithContext` suffix | `FetchWithContext`, `QueryContext` |
| Variant: in-place | `In` suffix | `SortIn()`, `ReverseIn()` |
| Variant: error | `Must` prefix | `MustParse()`, `MustLoadConfig()` |
| Option func | `With` + field name | `WithPort()`, `WithLogger()` |
| Enum (iota) | type name prefix, zero-value = unknown | `StatusUnknown` at 0, `StatusReady` |
| Named return | descriptive, for docs only | `(n int, err error)` |
| Error string | lowercase (incl. acronyms), no punctuation | `"image: unknown format"`, `"invalid id"` |
| Import alias | short, only on collision | `mrand "math/rand"`, `pb "app/proto"` |
| Format func | `f` suffix | `Errorf`, `Wrapf`, `Logf` |
| Test table fields | `got`/`expected` prefixes | `input string`, `expected int` |

### MixedCaps

All Go identifiers MUST use `MixedCaps` (or `mixedCaps`). NEVER use underscores in identifiers — the only exceptions are test function subcases (`TestFoo_InvalidInput`), generated code, and OS/cgo interop. This is load-bearing, not cosmetic — Go's export mechanism relies on capitalization, and tooling assumes MixedCaps throughout.

```go
// Good
MaxPacketSize
userCount
parseHTTPResponse

// Bad — these conventions conflict with Go's export mechanism and tooling expectations
MAX_PACKET_SIZE   // C/Python style
max_packet_size   // snake_case
kMaxBufferSize    // Hungarian notation
```

### Avoid Stuttering

Go call sites always include the package name, so repeating it in the identifier wastes the reader's time — `http.HTTPClient` forces parsing "HTTP" twice. A name MUST NOT repeat information already present in the package name, type name, or surrounding context.

```go
// Good — clean at the call site
http.Client       // not http.HTTPClient
json.Decoder      // not json.JSONDecoder
user.New()        // not user.NewUser()
config.Parse()    // not config.ParseConfig()

// In package sqldb:
type Connection struct{}  // not DBConnection — "db" is already in the package name

// Anti-stutter applies to ALL exported types, not just the primary struct:
// In package dbpool:
type Pool struct{}        // not DBPool
type Status struct{}      // not PoolStatus — callers write dbpool.Status
type Option func(*Pool)   // not PoolOption
```

### Frequently Missed Conventions

These conventions are correct but non-obvious — they are the most common source of naming mistakes:

**Constructor naming:** When a package exports a single primary type, the constructor is `New()`, not `NewTypeName()`. This avoids stuttering — callers write `apiclient.New()` not `apiclient.NewClient()`. Use `NewTypeName()` only when a package has multiple constructible types (like `http.NewRequest`, `http.NewServeMux`).

**Boolean struct fields:** Unexported boolean fields MUST use `is`/`has`/`can` prefix — `isConnected`, `hasPermission`, not bare `connected` or `permission`. The exported getter keeps the prefix: `IsConnected() bool`. This reads naturally as a question and distinguishes booleans from other types.

**Error strings are fully lowercase — including acronyms.** Write `"invalid message id"` not `"invalid message ID"`, because error strings are often concatenated with other context (`fmt.Errorf("parsing token: %w", err)`) and mixed case looks wrong mid-sentence. Sentinel errors should include the package name as prefix: `errors.New("apiclient: not found")`.

**Enum zero values:** Always place an explicit `Unknown`/`Invalid` sentinel at iota position 0. A `var s Status` silently becomes 0 — if that maps to a real state like `StatusReady`, code can behave as if a status was deliberately chosen when it wasn't.

**Subtest names:** Table-driven test case names in `t.Run()` should be fully lowercase descriptive phrases: `"valid id"`, `"empty input"` — not `"valid ID"` or `"Valid Input"`.

### Common Naming Mistakes

| Mistake | Fix |
| --- | --- |
| `ALL_CAPS` constants | Go reserves casing for visibility, not emphasis — use `MixedCaps` (`MaxRetries`) |
| `GetName()` getter | Go omits `Get` because `user.Name()` reads naturally at call sites. But `Is`/`Has`/`Can` prefixes are kept for boolean predicates: `IsHealthy() bool` not `Healthy() bool` |
| `Url`, `Http`, `Json` acronyms | Mixed-case acronyms create ambiguity (`HttpsUrl` — is it `Https+Url`?). Use all caps or all lower |
| `this` or `self` receiver | Go methods are called frequently — use 1-2 letter abbreviation (`s` for `Server`) to reduce visual noise |
| `util`, `helper` packages | These names say nothing about content — use specific names that describe the abstraction |
| `http.HTTPClient` stuttering | Package name is always present at call site — `http.Client` avoids reading "HTTP" twice |
| `user.NewUser()` constructor | Single primary type uses `New()` — `user.New()` avoids repeating the type name |
| `connected bool` field | Bare adjective is ambiguous — use `isConnected` so the field reads as a true/false question |
| `"invalid message ID"` error | Error strings must be fully lowercase including acronyms — `"invalid message id"` |
| `StatusReady` at iota 0 | Zero value should be a sentinel — `StatusUnknown` at 0 catches uninitialized values |
| `"not found"` error string | Sentinel errors should include the package name — `"mypackage: not found"` identifies the origin |
| `userSlice` type-in-name | Types encode implementation detail — `users` describes what it holds, not how |
| Inconsistent receiver names | Switching names across methods of the same type confuses readers — use one name consistently |
| `snake_case` identifiers | Underscores conflict with Go's MixedCaps convention and tooling expectations — use `mixedCaps` |
| Long names for short scopes | Name length should match scope — `i` is fine for a 3-line loop, `userIndex` is noise |
| Naming constants by value | Values change, roles don't — `DefaultPort` survives a port change, `Port8080` doesn't |
| `FetchCtx()` context variant | `WithContext` is the standard Go suffix — `FetchWithContext()` is instantly recognizable |
| `sort()` in-place but no `In` | Readers assume functions return new values. `SortIn()` signals mutation |
| `parse()` panicking on error | `MustParse()` warns callers that failure panics — surprises belong in the name |
| Mixing `With*`, `Set*`, `Use*` | Consistency across the codebase — `With*` is the Go convention for functional options |
| Plural package names | Go convention is singular (`net/url` not `net/urls`) — keeps import paths consistent |
| `Wrapf` without `f` suffix | The `f` suffix signals format-string semantics — `Wrapf`, `Errorf` tell callers to pass format args |
| Unnecessary import aliases | Aliases add cognitive load. Only alias on collision — `mrand "math/rand"` |
| Inconsistent concept names | Using `user`/`account`/`person` for the same concept forces readers to track synonyms — pick one name |

---

## Part 2 — Code Style

Style rules that require human judgment — linters handle formatting, this skill handles clarity.

> "Clear is better than clever." — Go Proverbs

When ignoring a rule, add a comment to the code.

### Line Length and Breaking

No rigid line limit, but lines beyond ~120 characters MUST be broken. Break at **semantic boundaries**, not arbitrary column counts. Function calls with 4+ arguments MUST use one argument per line — even when the prompt asks for single-line code:

```go
// Good — each argument on its own line, closing paren separate
mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
    handleUsers(
        w,
        r,
        serviceName,
        cfg,
        logger,
        authMiddleware,
    )
})
```

When a function signature is too long, the real fix is often **fewer parameters** (use an options struct) rather than better line wrapping. For multi-line signatures, put each parameter on its own line.

### Variable Declarations

SHOULD use `:=` for non-zero values, `var` for zero-value initialization. The form signals intent: `var` means "this starts at zero."

```go
var count int              // zero value, set later
name := "default"          // non-zero, := is appropriate
var buf bytes.Buffer       // zero value is ready to use
```

#### Slice and Map Initialization

Slices and maps MUST be initialized explicitly, never nil. Nil maps panic on write; nil slices serialize to `null` in JSON (vs `[]` for empty slices), surprising API consumers.

```go
users := []User{}                       // always initialized
m := map[string]int{}                   // always initialized
users := make([]User, 0, len(ids))      // preallocate when capacity is known
m := make(map[string]int, len(items))   // preallocate when size is known
```

Do not preallocate speculatively — `make([]T, 0, 1000)` wastes memory when the common case is 10 items.

#### Composite Literals

Composite literals MUST use field names — positional fields break when the type adds or reorders fields:

```go
srv := &http.Server{
    Addr:         ":8080",
    ReadTimeout:  5 * time.Second,
    WriteTimeout: 10 * time.Second,
}
```

### Control Flow

#### Reduce Nesting

Errors and edge cases MUST be handled first (early return). Keep the happy path at minimal indentation:

```go
func process(data []byte) (*Result, error) {
    if len(data) == 0 {
        return nil, errors.New("empty data")
    }

    parsed, err := parse(data)
    if err != nil {
        return nil, fmt.Errorf("parsing: %w", err)
    }

    return transform(parsed), nil
}
```

#### Eliminate Unnecessary else

When the `if` body ends with `return`/`break`/`continue`, the `else` MUST be dropped. Use default-then-override for simple assignments — assign a default, then override with independent conditions or a `switch`:

```go
// Good — default-then-override with switch (cleanest for mutually exclusive overrides)
level := slog.LevelInfo
switch {
case debug:
    level = slog.LevelDebug
case verbose:
    level = slog.LevelWarn
}

// Bad — else-if chain hides that there's a default
if debug {
    level = slog.LevelDebug
} else if verbose {
    level = slog.LevelWarn
} else {
    level = slog.LevelInfo
}
```

#### Complex Conditions and Init Scope

When an `if` condition has 3+ operands, MUST extract into named booleans — a wall of `||` is unreadable and hides business logic. Keep expensive checks inline for short-circuit benefit.

```go
// Good — named booleans make intent clear
isAdmin := user.Role == RoleAdmin
isOwner := resource.OwnerID == user.ID
isPublicVerified := resource.IsPublic && user.IsVerified
if isAdmin || isOwner || isPublicVerified || permissions.Contains(PermOverride) {
    allow()
}
```

Scope variables to `if` blocks when only needed for the check:

```go
if err := validate(input); err != nil {
    return err
}
```

#### Switch Over If-Else Chains

When comparing the same variable multiple times, prefer `switch`:

```go
switch status {
case StatusActive:
    activate()
case StatusInactive:
    deactivate()
default:
    panic(fmt.Sprintf("unexpected status: %d", status))
}
```

### Function Design

- Functions SHOULD be **short and focused** — one function, one job.
- Functions SHOULD have **<=4 parameters**. Beyond that, use an options struct.
- **Parameter order**: `context.Context` first, then inputs, then output destinations.
- Naked returns help in very short functions (1-3 lines) where return values are obvious, but become confusing when readers must scroll to find what's returned — name returns explicitly in longer functions.

```go
func FetchUser(ctx context.Context, id string) (*User, error)
func SendEmail(ctx context.Context, msg EmailMessage) error  // grouped into struct
```

#### Prefer range for Iteration

SHOULD use `range` over index-based loops. Use `range n` (Go 1.22+) for simple counting.

```go
for _, user := range users {
    process(user)
}
```

### Value vs Pointer Arguments

Pass small types (`string`, `int`, `bool`, `time.Time`) by value. Use pointers when mutating, for large structs (~128+ bytes), or when nil is meaningful.

### Code Organization Within Files

- **Group related declarations**: type, constructor, methods together
- **Order**: package doc, imports, constants, types, constructors, methods, helpers
- **One primary type per file** when it has significant methods
- **Blank imports** (`_ "pkg"`) register side effects (init functions). Restricting them to `main` and test packages makes side effects visible at the application root, not hidden in library code
- **Dot imports** pollute the namespace and make it impossible to tell where a name comes from — never use in library code
- **Unexport aggressively** — you can always export later; unexporting is a breaking change

### String Handling

Use `strconv` for simple conversions (faster), `fmt.Sprintf` for complex formatting. Use `%q` in error messages to make string boundaries visible. Use `strings.Builder` for loops, `+` for simple concatenation.

### Type Conversions

Prefer explicit, narrow conversions. Use generics over `any` when a concrete type will do:

```go
func Contains[T comparable](slice []T, target T) bool  // not []any
```

### Philosophy

- **"A little copying is better than a little dependency"**
- **Use `slices` and `maps` standard packages**; for filter/group-by/chunk, use `github.com/samber/lo`
- **"Reflection is never clear"** — avoid `reflect` unless necessary
- **Don't abstract prematurely** — extract when the pattern is stable
- **Minimize public surface** — every exported name is a commitment

---

## Part 3 — Error Handling

### Best Practices Summary

1. **Returned errors MUST always be checked** — NEVER discard with `_`
2. **Errors MUST be wrapped with context** using `fmt.Errorf("{context}: %w", err)`
3. **Error strings MUST be lowercase**, without trailing punctuation
4. **Use `%w` internally, `%v` at system boundaries** to control error chain exposure
5. **MUST use `errors.Is` for sentinel matching and `errors.As`/`errors.AsType` for typed chain inspection** instead of direct comparison or bare type assertions. For Go 1.26+, prefer `errors.AsType[T](err)` when `T` implements `error`; use `errors.As(err, &target)` for Go <1.26 or for non-error interface targets.
6. **SHOULD use `errors.Join`** (Go 1.20+) to combine independent errors
7. **Errors MUST be either logged OR returned**, NEVER both (single handling rule)
8. **Use sentinel errors** for expected conditions, custom types for carrying data
9. **NEVER use `panic` for expected error conditions** — reserve for truly unrecoverable states
10. **SHOULD use `slog`** (Go 1.21+) for structured error logging — not `fmt.Println` or `log.Printf`
11. **Use `samber/oops`** for production errors needing stack traces, user/tenant context, or structured attributes
12. **Log HTTP requests** with structured middleware capturing method, path, status, and duration
13. **Use log levels** to indicate error severity
14. **Never expose technical errors to users** — translate internal errors to user-friendly messages, log technical details separately
15. **Keep log grouping low-cardinality** — at logging/APM boundaries, keep message templates stable and attach IDs, paths, line numbers, and counts as structured attributes. Error values may include useful operational context, but avoid putting high-cardinality data into the stable log message used for grouping.

### Error Creation

Error messages should be lowercase, no punctuation, and describe what happened without prescribing action. Covers sentinel errors (one-time preallocation for performance), custom error types (for carrying rich context).

### Error Wrapping and Inspection

`fmt.Errorf("{context}: %w", err)` preserves chains (vs `%v` which concatenates). Inspect chains with `errors.Is`, `errors.As`, and Go 1.26+ `errors.AsType` for type-safe error handling. Use `errors.Join` for combining independent errors.

### Error Handling Patterns and Logging

The single handling rule: errors are either logged OR returned, NEVER both (prevents duplicate logs cluttering aggregators). Panic/recover design, `samber/oops` for production errors, and `slog` structured logging integration for APM tools.

### References

- [lmittmann/tint](https://github.com/lmittmann/tint)
- [samber/oops](https://github.com/samber/oops)
- [samber/slog-multi](https://github.com/samber/slog-multi)
- [samber/slog-sampling](https://github.com/samber/slog-sampling)
- [samber/slog-formatter](https://github.com/samber/slog-formatter)
- [samber/slog-http](https://github.com/samber/slog-http)
- [samber/slog-sentry](https://github.com/samber/slog-sentry)
- [log/slog package](https://pkg.go.dev/log/slog)
