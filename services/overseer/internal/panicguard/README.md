`panicguard` is the one place the overseer recovers a panic and the one way it runs goroutines it waits for.

- `Try` runs a function and returns the panic as an error, `Call` does the same for a function that returns a value, and `Recovered` reads the panic value back out of an error.
- `Contain` runs a function and logs a panic once at error level with the attributes it is given, for work whose failure must not stop the caller.
- `Group` runs functions in goroutines. `Wait` joins them and returns every panic it contained, so one goroutine's panic never takes down its siblings.
- `Once` starts a goroutine at most once however often and from however many callers it is asked: `Go` reports a panic through `Wait`, and `Contain` logs it. A bucket's `Start` uses it. One `Once` runs one function, so use either `Go` or `Contain` on it, never both.
- `WaitWithin` waits for a function to return and reports whether it did within a budget, for a shutdown that must not hang.
- `recover` and `sync.WaitGroup` are banned everywhere else by `forbidigo`.
