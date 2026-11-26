`polltest` owns how a test waits for something that happens on its own schedule: a goroutine reaching a state, a log line, a row lock, a stream slot.

- `Until(t, within, what, cond)` checks `cond` every few milliseconds and fails the test with `what` if it is still false after `within`. It never calls `time.Sleep`.
- `Within(within, cond)` is the same wait that returns whether `cond` came true, for a test that wants to assert on the outcome itself.

Prefer a `testing/synctest` bubble when the code under test only uses fake-able time and in-memory I/O, and a channel handshake when the code can signal; use `polltest` for real databases, real sockets and kernel objects, where nothing can be signalled and fake time does not advance.
