package memory

type TB interface {
	Helper()
	Cleanup(func())
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}
