---
paths: ["services/go-api/**/*.go"]
---

# Go patterns: safety, concurrency, design, testing

---

## Part 1 — Safety: Correctness & Defensive Coding

Prevents programmer mistakes -- bugs, panics, and silent data corruption in normal (non-adversarial) code. Security handles attackers; safety handles ourselves.

### Safety Best Practices Summary

1. **Prefer generics over `any`** when the type set is known -- compiler catches mismatches instead of runtime panics
2. **Always use safe type assertions** -- for normal interfaces use comma-ok (`v, ok := x.(T)`); for reflection in Go 1.25+ prefer `reflect.TypeAssert[T](value)` over `value.Interface().(T)`.
3. **Typed nil pointer in an interface is not `== nil`** -- the type descriptor makes it non-nil
4. **Writing to a nil map panics** -- always initialize before use
5. **`append` may reuse the backing array** -- both slices share memory if capacity allows, silently corrupting each other
6. **Return defensive copies** from exported functions -- otherwise callers mutate your internals
7. **`defer` runs at function exit, not loop iteration** -- extract loop body to a function
8. **Integer conversions truncate silently** -- `int64` to `int32` wraps without error
9. **Float arithmetic is not exact** -- use epsilon comparison or `math/big`
10. **Design useful zero values** -- nil map fields panic on first write; use lazy init
11. **Use `sync.Once` for lazy init** -- guarantees exactly-once even under concurrency

### Nil Safety

Nil-related panics are the most common crash in Go.

#### The nil interface trap

Interfaces store (type, value). An interface is `nil` only when both are nil. Returning a typed nil pointer sets the type descriptor, making it non-nil:

```go
// BAD -- interface{type: *MyHandler, value: nil} is not == nil
func getHandler() http.Handler {
    var h *MyHandler // nil pointer
    if !enabled {
        return h // interface{type: *MyHandler, value: nil} != nil
    }
    return h
}

// GOOD -- return nil explicitly
func getHandler() http.Handler {
    if !enabled {
        return nil // interface{type: nil, value: nil} == nil
    }
    return &MyHandler{}
}
```

#### Nil map, slice, and channel behavior

| Type    | Index into nil | Write to nil   | Len/Cap of nil | Range over nil |
| ------- | -------------- | -------------- | -------------- | -------------- |
| Map     | Zero value     | **panic**      | 0              | 0 iterations   |
| Slice   | **panic**      | **panic**      | 0              | 0 iterations   |
| Channel | Blocks forever | Blocks forever | 0              | Blocks forever |

```go
// BAD -- nil map panics on write
var m map[string]int
m["key"] = 1

// GOOD -- initialize or lazy-init in methods
m := make(map[string]int)

func (r *Registry) Add(name string, val int) {
    if r.items == nil { r.items = make(map[string]int) }
    r.items[name] = val
}
```

### Slice & Map Safety

#### Slice aliasing -- the append trap

`append` reuses the backing array if capacity allows. Both slices then share memory:

```go
// BAD -- a and b share backing array
a := make([]int, 3, 5)
b := append(a, 4)
b[0] = 99 // also modifies a[0]

// GOOD -- full slice expression forces new allocation
b := append(a[:len(a):len(a)], 4)
```

### Numeric Safety

#### Implicit type conversions truncate silently

```go
// BAD -- silently wraps around if val > math.MaxInt32 (3B becomes -1.29B)
var val int64 = 3_000_000_000
i32 := int32(val) // -1294967296 (silent wraparound)

// GOOD -- check before converting
if val > math.MaxInt32 || val < math.MinInt32 {
    return fmt.Errorf("value %d overflows int32", val)
}
i32 := int32(val)
```

#### Float comparison

```go
// BAD -- floating point arithmetic is not exact
var a, b, c float64 = 0.1, 0.2, 0.3
a+b == c // false

// GOOD -- use epsilon comparison
const epsilon = 1e-9
math.Abs((a+b)-c) < epsilon // true
```

#### Division by zero

Integer division by zero panics. Float division by zero produces `+Inf`, `-Inf`, or `NaN`.

```go
func avg(total, count int) (int, error) {
    if count == 0 {
        return 0, errors.New("division by zero")
    }
    return total / count, nil
}
```

### Resource Safety

#### defer in loops -- resource accumulation

`defer` runs at _function_ exit, not loop iteration. Resources accumulate until the function returns:

```go
// BAD -- all files stay open until function returns
for _, path := range paths {
    f, _ := os.Open(path)
    defer f.Close() // deferred until function exits
    process(f)
}

// GOOD -- extract to function so defer runs per iteration
for _, path := range paths {
    if err := processOne(path); err != nil { return err }
}
func processOne(path string) error {
    f, err := os.Open(path)
    if err != nil { return err }
    defer f.Close()
    return process(f)
}
```

### Immutability & Defensive Copying

Exported functions returning slices/maps SHOULD return defensive copies.

#### Protecting struct internals

```go
// BAD -- exported slice field, anyone can mutate
type Config struct {
    Hosts []string
}

// GOOD -- unexported field with accessor returning a copy
type Config struct {
    hosts []string
}

func (c *Config) Hosts() []string {
    return slices.Clone(c.hosts)
}
```

### Initialization Safety

#### Zero-value design

Design types so `var x MyType` is safe -- prevents "forgot to initialize" bugs:

```go
var mu sync.Mutex   // usable at zero value
var buf bytes.Buffer // usable at zero value

// BAD -- nil map panics on write
type Cache struct { data map[string]any }
```

#### sync.Once for lazy initialization

```go
type DB struct {
    once sync.Once
    conn *sql.DB
}

func (db *DB) connection() *sql.DB {
    db.once.Do(func() {
        db.conn, _ = sql.Open("postgres", connStr)
    })
    return db.conn
}
```

#### Go 1.25+ reflection type assertions

For reflection code, prefer `reflect.TypeAssert[T]` over `value.Interface().(T)`.

```go
v := reflect.ValueOf(x)
if s, ok := reflect.TypeAssert[string](v); ok {
    use(s)
}
```

### Safety Common Mistakes

| Mistake | Fix |
| --- | --- |
| Bare type assertion `v := x.(T)` | Panics on type mismatch. Use `v, ok := x.(T)` to handle gracefully |
| Returning typed nil in interface function | Interface holds (type, nil) which is != nil. Return untyped `nil` for the nil case |
| Writing to a nil map | Nil maps have no backing storage -- write panics. Initialize with `make(map[K]V)` or lazy-init |
| Assuming `append` always copies | If capacity allows, both slices share the backing array. Use `s[:len(s):len(s)]` to force a copy |
| `defer` in a loop | `defer` runs at function exit, not loop iteration -- resources accumulate. Extract body to a separate function |
| `int64` to `int32` without bounds check | Values wrap silently (3B to -1.29B). Check against `math.MaxInt32`/`math.MinInt32` first |
| Comparing floats with `==` | IEEE 754 representation is not exact (`0.1+0.2 != 0.3`). Use `math.Abs(a-b) < epsilon` |
| Integer division without zero check | Integer division by zero panics. Guard with `if divisor == 0` before dividing |
| Returning internal slice/map reference | Callers can mutate your struct's internals through the shared backing array. Return a defensive copy |
| Blocking forever on nil channel | Nil channels block on both send and receive. Always initialize before use |

---

## Part 2 — Concurrency

Go's concurrency model is built on goroutines and channels. Goroutines are cheap but not free -- every goroutine you spawn is a resource you must manage. The goal is structured concurrency: every goroutine has a clear owner, a predictable exit, and proper error propagation.

### Concurrency Core Principles

1. **Every goroutine must have a clear exit** -- without a shutdown mechanism (context, done channel, WaitGroup), they leak and accumulate until the process crashes
2. **Share memory by communicating** -- channels transfer ownership explicitly; mutexes protect shared state but make ownership implicit
3. **Send copies, not pointers** on channels -- sending pointers creates invisible shared memory, defeating the purpose of channels
4. **Only the sender closes a channel** -- closing from the receiver side panics if the sender writes after close
5. **Specify channel direction** (`chan<-`, `<-chan`) -- the compiler prevents misuse at build time
6. **Default to unbuffered channels** -- larger buffers mask backpressure; use them only with measured justification
7. **Always include `ctx.Done()` in select** -- without it, goroutines leak after caller cancellation
8. **Avoid repeated `time.After` in hot loops** -- each call allocates a timer and creates unnecessary churn; use `time.NewTimer` + `Reset` for long-running loops
9. **Track goroutine leaks in tests** with `go.uber.org/goleak`

### Channel vs Mutex vs Atomic

| Scenario | Use | Why |
| --- | --- | --- |
| Passing data between goroutines | Channel | Communicates ownership transfer |
| Coordinating goroutine lifecycle | Channel + context | Clean shutdown with select |
| Protecting shared struct fields | `sync.Mutex` / `sync.RWMutex` | Simple critical sections |
| Simple counters, flags | `sync/atomic` | Lock-free, lower overhead |
| Many readers, few writers on a map | `sync.Map` | Optimized for read-heavy workloads. **Concurrent map read/write causes a hard crash** |
| Caching expensive computations | `sync.Once` / `singleflight` | Execute once or deduplicate |

### WaitGroup vs errgroup

| Need | Use | Why |
| --- | --- | --- |
| Wait for goroutines, errors not needed | `sync.WaitGroup` | Fire-and-forget |
| Wait + collect first error | `errgroup.Group` | Error propagation |
| Wait + cancel siblings on first error | `errgroup.WithContext` | Context cancellation on error |
| Wait + limit concurrency | `errgroup.SetLimit(n)` | Built-in worker pool |

### Sync Primitives Quick Reference

| Primitive | Use case | Key notes |
| --- | --- | --- |
| `sync.Mutex` | Protect shared state | Keep critical sections short; never hold across I/O |
| `sync.RWMutex` | Many readers, few writers | Never upgrade RLock to Lock (deadlock) |
| `sync/atomic` | Simple counters, flags | Prefer typed atomics (Go 1.19+): `atomic.Int64`, `atomic.Bool` |
| `sync.Map` | Concurrent map, read-heavy | No explicit locking; use `RWMutex`+map when writes dominate |
| `sync.Pool` | Reuse temporary objects | Always `Reset()` before `Put()`; reduces GC pressure |
| `sync.Once` | One-time initialization | Go 1.21+: `OnceFunc`, `OnceValue`, `OnceValues` |
| `sync.WaitGroup` | Waiting for simple goroutines | Go 1.25+: prefer `wg.Go(func(){ ... })` for fire-and-wait tasks that do not panic and do not need error propagation. For Go <1.25 use `Add`/`Done`. For errors/cancellation/limits, use `errgroup` with context. |
| `x/sync/singleflight` | Deduplicate concurrent calls | Cache stampede prevention |
| `x/sync/errgroup` | Goroutine group + errors | `SetLimit(n)` replaces hand-rolled worker pools |

### Concurrency Checklist

Before spawning a goroutine, answer:

- [ ] **How will it exit?** -- context cancellation, channel close, or explicit signal
- [ ] **Can I signal it to stop?** -- pass `context.Context` or done channel
- [ ] **Can I wait for it?** -- `sync.WaitGroup` or `errgroup`
- [ ] **Who owns the channels?** -- creator/sender owns and closes
- [ ] **Should this be synchronous instead?** -- don't add concurrency without measured need

### Go 1.26 experimental goroutine leak profile

For Go 1.26 diagnostics, there is an experimental goroutine leak profile. It is gated by `GOEXPERIMENT=goroutineleakprofile`; do not rely on it as default stable behavior.

Typical usage when the experiment is enabled:

```bash
curl http://localhost:6060/debug/pprof/goroutineleak?debug=2
go tool pprof http://localhost:6060/debug/pprof/goroutineleak
```

Keep existing tools:

- tests: `go.uber.org/goleak`
- runtime count: `runtime.NumGoroutine()`
- stack dump: `/debug/pprof/goroutine?debug=2`
- race checks: `go test -race ./...`

### Concurrency Common Mistakes

| Mistake | Fix |
| --- | --- |
| Fire-and-forget goroutine | Provide stop mechanism (context, done channel) |
| Closing channel from receiver | Only the sender closes |
| `time.After` in hot loop | Reuse `time.NewTimer` + `Reset` |
| Missing `ctx.Done()` in select | Always select on context to allow cancellation |
| Unbounded goroutine spawning | Use `errgroup.SetLimit(n)` or semaphore |
| Sharing pointer via channel | Send copies or immutable values |
| `wg.Add` inside goroutine | Call `Add` before `go` -- `Wait` may return early otherwise |
| Forgetting `-race` in CI | Always run `go test -race ./...` |
| Mutex held across I/O | Keep critical sections short |

---

## Part 3 — Design Patterns & Idioms

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

---

## Part 4 — Testing

### Testing Best Practices Summary

1. Table-driven tests MUST use named subtests -- every test case needs a `name` field passed to `t.Run`
2. Integration tests MUST use build tags (`//go:build integration`) to separate from unit tests
3. Tests MUST NOT depend on execution order -- each test MUST be independently runnable
4. Independent tests SHOULD use `t.Parallel()` when possible
5. NEVER test implementation details -- test observable behavior and public API contracts
6. Packages with goroutines SHOULD use `goleak.VerifyTestMain` in `TestMain` to detect goroutine leaks
7. Use testify as helpers, not a replacement for standard library
8. Mock interfaces, not concrete types
9. Keep unit tests fast (< 1ms), use build tags for integration tests
10. Run tests with race detection in CI
11. Include examples as executable documentation

### Test Structure and Organization

#### File Conventions

```go
// package_test.go - tests in same package (white-box, access unexported)
package mypackage

// mypackage_test.go - tests in test package (black-box, public API only)
package mypackage_test
```

#### Naming Conventions

```go
func TestAdd(t *testing.T) { ... }               // function test
func TestMyStruct_MyMethod(t *testing.T) { ... } // method test
func BenchmarkAdd(b *testing.B) { ... }          // benchmark
func ExampleAdd() { ... }                        // example
func FuzzAdd(f *testing.F) { ... }               // fuzz test
```

### Table-Driven Tests

Table-driven tests are the idiomatic Go way to test multiple scenarios. Always name each test case.

```go
func TestCalculatePrice(t *testing.T) {
    tests := []struct {
        name     string
        quantity int
        unitPrice float64
        expected  float64
    }{
        {
            name:      "single item",
            quantity:  1,
            unitPrice: 10.0,
            expected:  10.0,
        },
        {
            name:      "bulk discount - 100 items",
            quantity:  100,
            unitPrice: 10.0,
            expected:  900.0, // 10% discount
        },
        {
            name:      "zero quantity",
            quantity:  0,
            unitPrice: 10.0,
            expected:  0.0,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := CalculatePrice(tt.quantity, tt.unitPrice)
            if got != tt.expected {
                t.Errorf("CalculatePrice(%d, %.2f) = %.2f, want %.2f",
                    tt.quantity, tt.unitPrice, got, tt.expected)
            }
        })
    }
}
```

### Goroutine Leak Detection with goleak

Use `go.uber.org/goleak` to detect leaking goroutines, especially for concurrent code:

```go
import (
    "testing"
    "go.uber.org/goleak"
)

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

To exclude specific goroutine stacks (for known leaks or library goroutines):

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m,
        goleak.IgnoreCurrent(),
    )
}
```

Or per-test:

```go
func TestWorkerPool(t *testing.T) {
    defer goleak.VerifyNone(t)
    // ... test code ...
}
```

### testing/synctest for Deterministic Goroutine Testing

`testing/synctest` (Go 1.25+) provides deterministic tests for goroutines, timers, deadlines, and context cancellation. Time advances only when all goroutines are blocked, making ordering predictable.

When to use `synctest` instead of real time:

- Testing concurrent code with time-based operations (time.Sleep, time.After, time.Ticker)
- When race conditions need to be reproducible
- When tests are flaky due to timing issues

```go
import (
    "context"
    "testing"
    "testing/synctest"
    "time"
)

func TestContextTimeout(t *testing.T) {
    synctest.Test(t, func(t *testing.T) {
        const timeout = 5 * time.Second

        ctx, cancel := context.WithTimeout(t.Context(), timeout)
        defer cancel()

        time.Sleep(timeout - time.Nanosecond)
        synctest.Wait()
        if err := ctx.Err(); err != nil {
            t.Fatalf("before timeout: %v", err)
        }

        time.Sleep(time.Nanosecond)
        synctest.Wait()
        if err := ctx.Err(); err != context.DeadlineExceeded {
            t.Fatalf("after timeout: got %v, want DeadlineExceeded", err)
        }
    })
}
```

Use `synctest.Test` in Go 1.25+ and Go 1.26+. Do not use the old Go 1.24 experimental `synctest.Run` API in Go 1.25+ or Go 1.26+ code.

Key differences in `synctest`:

- `time.Sleep` advances synthetic time instantly when the goroutine blocks
- `time.After` fires when synthetic time reaches the duration
- All goroutines run to blocking points before time advances
- Test execution is deterministic and repeatable

### Benchmarks

Write benchmarks to measure performance and detect regressions:

```go
func BenchmarkStringConcatenation(b *testing.B) {
    b.Run("plus-operator", func(b *testing.B) {
        for b.Loop() {
            result := "a" + "b" + "c"
            _ = result
        }
    })

    b.Run("strings.Builder", func(b *testing.B) {
        for b.Loop() {
            var builder strings.Builder
            builder.WriteString("a")
            builder.WriteString("b")
            builder.WriteString("c")
            _ = builder.String()
        }
    })
}
```

Benchmarks with different input sizes:

```go
func BenchmarkFibonacci(b *testing.B) {
    sizes := []int{10, 20, 30}
    for _, size := range sizes {
        b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
            b.ReportAllocs()
            for b.Loop() {
                Fibonacci(size)
            }
        })
    }
}
```

For Go 1.24+, new benchmarks should use `b.Loop()`. Use legacy `b.N` loops only when the module targets Go <1.24 or when preserving old benchmark code intentionally.

#### Go 1.26+: test artifacts

When a test, benchmark, or fuzz target needs to persist files for inspection, use `ArtifactDir()` instead of ad-hoc paths or repo-local output.

```go
func TestRenderGoldenArtifact(t *testing.T) {
    dir := t.ArtifactDir()

    out := filepath.Join(dir, "rendered.json")
    if err := os.WriteFile(out, renderedBytes, 0o644); err != nil {
        t.Fatal(err)
    }

    t.Logf("artifact written: %s", out)
}
```

Available on `*testing.T`, `*testing.B`, and `*testing.F` in Go 1.26+.

### Parallel Tests

Use `t.Parallel()` to run tests concurrently:

```go
func TestParallelOperations(t *testing.T) {
    tests := []struct {
        name string
        data []byte
    }{
        {"small data", make([]byte, 1024)},
        {"medium data", make([]byte, 1024*1024)},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            is := assert.New(t)

            result := Process(tt.data)
            is.NotNil(result)
        })
    }
}
```

### Fuzzing

Use fuzzing to find edge cases and bugs:

```go
func FuzzReverse(f *testing.F) {
    f.Add("hello")
    f.Add("")
    f.Add("a")

    f.Fuzz(func(t *testing.T, input string) {
        reversed := Reverse(input)
        doubleReversed := Reverse(reversed)
        if input != doubleReversed {
            t.Errorf("Reverse(Reverse(%q)) = %q, want %q", input, doubleReversed, input)
        }
    })
}
```

### Examples as Documentation

Examples are executable documentation verified by `go test`:

```go
func ExampleCalculatePrice() {
    price := CalculatePrice(100, 10.0)
    fmt.Printf("Price: %.2f\n", price)
    // Output: Price: 900.00
}

func ExampleCalculatePrice_singleItem() {
    price := CalculatePrice(1, 25.50)
    fmt.Printf("Price: %.2f\n", price)
    // Output: Price: 25.50
}
```

### Code Coverage

```bash
# Generate coverage file
go test -coverprofile=coverage.out ./...

# View coverage in HTML
go tool cover -html=coverage.out

# Coverage by function
go tool cover -func=coverage.out

# Total coverage percentage
go tool cover -func=coverage.out | grep total
```

### Integration Tests

Use build tags to separate integration tests from unit tests:

```go
//go:build integration

package mypackage

func TestDatabaseIntegration(t *testing.T) {
    db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
    if err != nil {
        t.Fatal(err)
    }
    defer db.Close()

    // Test real database operations
}
```

Run integration tests separately:

```bash
go test -tags=integration ./...
```

### Mocking

Mock interfaces, not concrete types. Define interfaces where consumed, then create mock implementations.

### Testing Quick Reference

```bash
go test ./...                          # all tests
go test -run TestName ./...            # specific test by exact name
go test -run TestName/subtest ./...    # subtests within a test
go test -run 'Test(Add|Sub)' ./...     # multiple tests (regexp OR)
go test -run 'Test[A-Z]' ./...        # tests starting with capital letter
go test -run 'TestUser.*' ./...        # tests matching prefix
go test -run '.*Validation.*' ./...    # tests containing substring
go test -run TestName/. ./...          # all subtests of TestName
go test -run '/(unit|integration)' ./... # filter by subtest name
go test -race ./...                    # race detection
go test -cover ./...                   # coverage summary
go test -bench=. -benchmem ./...       # benchmarks
go test -fuzz=FuzzName ./...           # fuzzing
go test -tags=integration ./...        # integration tests
```
