package connections_test

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"strings"
	"testing"
)

func TestDeps_MissingHandlesNamesTheTrackerWhenItIsAbsent(t *testing.T) {
	err := connections.Deps{}.MissingHandles()

	if err == nil || !strings.Contains(err.Error(), "Tracker") {
		t.Fatalf("MissingHandles = %v, want an error naming Tracker", err)
	}
}
