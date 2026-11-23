`runloop` owns how background jobs run on a schedule under leadership, and the one panic guard every goroutine goes through.

`Try` and `RecoverContext` recover a panic and log it once, at ERROR with the caller's context, the stack and the panic value with secrets masked. `Try` also returns it as a `*PanicError` so the caller can decide the outcome; the caller does not log it again.

`Detach` is the one way for work to outlive the request that started it: it keeps the request's values (the correlation id) and runs under its own budget. `WaitWithin` waits for work until a deadline and says whether it finished. When the deadline wins, the wait keeps running in its own goroutine until it returns, so only pass a wait that does end.
