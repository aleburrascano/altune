`telemetry` owns which events and errors the app reports, and how they survive a lost connection.

State lives in an instance built by `createTelemetry(deps)`. `createAppTelemetry(overrides)` wires the real clock, network, file store, app state and session; tests pass only what they want to fake. `src/mobile/RootLayout` installs one instance with `installTelemetry` (a call before any install builds the app instance on first use), and `jest/setup-after-env.js` installs a fresh in-memory one before every test.

Known debt: `installTelemetry` plus the free functions (`recordEvent`, `enqueueCritical`, `reportClientError`, ...) that delegate to the installed instance are still a process-wide slot, a global with a tidier door. Step 2, done module by module as each caller is rewritten: callers get telemetry from a provider (`useTelemetry()`) or as an argument, and each free function goes away once its last caller moves.
