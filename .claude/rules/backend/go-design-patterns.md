---
paths: ["services/go-api/**/*.go"]
---

# Go design patterns and idioms

Idiomatic Go patterns for production-ready code.

### Design Best Practices Summary

1. Constructors SHOULD use **functional options** -- they scale better as APIs evolve (one function per option, no breaking changes)
2. Functional options MUST **return an error** if validation can fail -- catch bad config at construction, not at runtime
3. **Avoid `init()`** -- runs implicitly, cannot return errors, makes testing unpredictable. Use explicit constructors
4. Enums SHOULD **start at 1** (or Unknown sentinel at 0) -- Go's zero value silently passes as the first enum member
5. Error cases MUST be **handled first** with early return -- keep happy path flat
6. **Panic is for bugs, not expected errors** -- callers can handle returned errors; panics crash the process
7. **`defer Close()` immediately after opening** -- later code changes can accidentally skip cleanup
8. **`runtime.AddCleanup`** over `runtime.SetFinalizer` -- finalizers are unpredictable and can resurrect objects
9. Every external call SHOULD **have a timeout** -- a slow upstream hangs your goroutine indefinitely
10. **Limit everything** (pool sizes, queue depths, buffers) -- unbounded resources grow until they crash
11. Retry logic MUST **check context cancellation** between attempts
12. **Use `strings.Builder`** for concatenation in loops
13. string vs []byte: **use `[]byte` for mutation and I/O**, `string` for display and keys -- conversions allocate
14. Iterators (Go 1.23+): **use for lazy evaluation** -- avoid loading everything into memory
15. **Stream large transfers** -- loading millions of rows causes OOM; stream keeps memory constant
16. `//go:embed` for **static assets** -- embeds at compile time, eliminates runtime file I/O errors
17. **Use `crypto/rand`** for keys/tokens -- `math/rand` is predictable
18. Regexp MUST be **compiled once at package level** -- compilation is O(n) and allocates
19. Compile-time interface checks: **`var _ Interface = (*Type)(nil)`**
20. **A little recode > a big dependency** -- each dep adds attack surface and maintenance burden
21. **Design for testability** -- accept interfaces, inject dependencies

### Constructor Patterns: Functional Options (Preferred)

```go
type Server struct {
    addr         string
    readTimeout  time.Duration
    writeTimeout time.Duration
    maxConns     int
}

type Option func(*Server)

func WithReadTimeout(d time.Duration) Option {
    return func(s *Server) { s.readTimeout = d }
}

func WithWriteTimeout(d time.Duration) Option {
    return func(s *Server) { s.writeTimeout = d }
}

func WithMaxConns(n int) Option {
    return func(s *Server) { s.maxConns = n }
}

func NewServer(addr string, opts ...Option) *Server {
    // Default options
    s := &Server{
        addr:         addr,
        readTimeout:  5 * time.Second,
        writeTimeout: 10 * time.Second,
        maxConns:     100,
    }
    for _, opt := range opts {
        opt(s)
    }
    return s
}

// Usage
srv := NewServer(":8080",
    WithReadTimeout(30*time.Second),
    WithMaxConns(500),
)
```

Use builder pattern only if you need complex validation between configuration steps.

### Constructors & Initialization

#### Avoid `init()` and Mutable Globals

`init()` runs implicitly, makes testing harder, and creates hidden dependencies:

- Multiple `init()` functions run in declaration order, across files in **filename alphabetical order** -- fragile
- Cannot return errors -- failures must panic or `log.Fatal`
- Runs before `main()` and tests -- side effects make tests unpredictable

```go
// Bad -- hidden global state
var db *sql.DB

func init() {
    var err error
    db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
}

// Good -- explicit initialization, injectable
func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}
```

#### Enums: Start at 1

Zero values should represent invalid/unset state:

```go
type Status int

const (
    StatusUnknown Status = iota // 0 = invalid/unset
    StatusActive                // 1
    StatusInactive              // 2
    StatusSuspended             // 3
)
```

#### Compile Regexp Once

```go
// Good -- compiled once at package level
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func ValidateEmail(email string) bool {
    return emailRegex.MatchString(email)
}
```

#### Use `//go:embed` for Static Assets

```go
import "embed"

//go:embed templates/*
var templateFS embed.FS

//go:embed version.txt
var version string
```

### Error Flow Patterns

Error cases MUST be handled first with early return -- keep the happy path at minimal indentation.

#### When to Panic vs Return Error

- **Return error**: network failures, file not found, invalid input -- anything a caller can handle
- **Panic**: nil pointer in a place that should be impossible, violated invariant, `Must*` constructors used at init time
- **`.Close()` / `Flush()` errors**: read-only cleanup can often use `defer f.Close()`, but write/flush resources must report close or flush errors when durability matters

### Data Handling

#### string vs []byte vs []rune

| Type     | Default for | Use when                                            |
| -------- | ----------- | --------------------------------------------------- |
| `string` | Everything  | Immutable, safe, UTF-8                              |
| `[]byte` | I/O         | Writing to `io.Writer`, building strings, mutations |
| `[]rune` | Unicode ops | `len()` must mean characters, not bytes             |

Avoid repeated conversions -- each one allocates. Stay in one type until you need the other.

#### Iterators & Streaming for Large Data

Use iterators (Go 1.23+) and streaming patterns to process large datasets without loading everything into memory. For large transfers between services (e.g., 1M rows DB to HTTP), stream to prevent OOM.

### Resource Management

`defer Close()` immediately after opening -- don't wait, don't forget:

```go
f, err := os.Open(path)
if err != nil {
    return err
}
defer f.Close() // right here, not 50 lines later

rows, err := db.QueryContext(ctx, query)
if err != nil {
    return err
}
defer rows.Close()
```

### Resilience & Limits

#### Timeout Every External Call

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

resp, err := httpClient.Do(req.WithContext(ctx))
```

#### Retry & Context Checks

Retry logic MUST check `ctx.Err()` between attempts and use exponential/linear backoff via `select` on `ctx.Done()`. Long loops MUST check `ctx.Err()` periodically.

### Architecture

Core principles regardless of architecture:

- **Keep domain pure** -- no framework dependencies in the domain layer
- **Fail fast** -- validate at boundaries, trust internal code
- **Make illegal states unrepresentable** -- use types to enforce invariants

### Code Philosophy

- **Avoid repetitive code** -- but don't abstract prematurely
- **Minimize dependencies** -- a little recode > a big dependency
- **Design for testability** -- accept interfaces, inject dependencies, keep functions pure
