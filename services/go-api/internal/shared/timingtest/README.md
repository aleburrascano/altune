Infrastructure (test helper): `timingtest` owns how a test deals with real time and real sockets: waiting for something that happens on its own schedule, and seeing a write deadline fire on a slow client.

- `Until(t, within, what, cond)` checks `cond` every few milliseconds and fails the test with `what` if it is still false after `within`. It never calls `time.Sleep`.
- `Within(within, cond)` is the same wait that returns whether `cond` came true, for a test that wants to assert on the outcome itself.
- `NewSmallBufferServer`, `SendGet` and `ReadResponse` give a test server and client with small socket buffers, so tests can see a write deadline fire.

Prefer a `testing/synctest` bubble when the code under test only uses fake-able time and in-memory I/O, and a channel handshake when the code can signal; use `Until` for real databases, real sockets and kernel objects, where nothing can be signalled and fake time does not advance.
