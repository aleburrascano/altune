`panicguard` is the one place the overseer recovers a panic and the one way it runs goroutines it waits for.

- `Try` runs a function and returns the panic as an error, `Call` does the same for a function that returns a value, and `Recovered` reads the panic value back out of an error.
- `Contain` runs a function and logs a panic once at error level with the attributes it is given, for work whose failure must not stop the caller.
- `Group` runs functions in goroutines. `Wait` joins them and returns every panic it contained, so one goroutine's panic never takes down its siblings.
- `recover` and `sync.WaitGroup` are banned everywhere else by `forbidigo`.
