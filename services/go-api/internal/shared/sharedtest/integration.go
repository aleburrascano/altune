package sharedtest

import (
	"os"
	"testing"
)

func RequireIntegration(t testing.TB) {
	t.Helper()
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run integration tests")
	}
}
