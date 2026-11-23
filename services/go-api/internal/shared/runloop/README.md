`runloop` owns how background jobs run on a schedule under leadership, and the one panic guard every goroutine goes through.

`Try` and `RecoverContext` recover a panic and log it once, at ERROR with the caller's context and the stack. `Try` also returns it as a `*PanicError` so the caller can decide the outcome; the caller does not log it again.
