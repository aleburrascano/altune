---
paths: ["services/go-api/**/*.go"]
---

# Go concurrency

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
