package memory

import "context"

type TB interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Cleanup(func())
	Context() context.Context
}
