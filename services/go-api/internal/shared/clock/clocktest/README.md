`clocktest.Manual` is the one fake clock for tests of code that takes a `clock.Func`. It starts at the time it is given, moves only when the test calls `Advance` or `Set`, and is safe to read and advance from many goroutines. Pass `m.Func()` (or `m.Now`) where the code under test wants a `clock.Func`.

Production code never imports this package. A test that needs a clock which ticks on every read, or one that fails, writes that behaviour next to the test that needs it.
