Infrastructure: `childprocess` owns how child processes start, time out and stop as a group.

No other production package calls `exec.Command` or `exec.CommandContext` (forbidigo in `.golangci.strict.yml`); the `scripts` main packages and `*test` packages are the exceptions. `Run`, `RunCapture` and `RunWithTimeout` are the way in.
