package handles_test

import (
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"strings"
	"testing"
)

func TestDeps_MissingHandlesNamesTheTrackerWhenItIsAbsent(t *testing.T) {
	err := handles.Deps{}.MissingHandles()

	if err == nil || !strings.Contains(err.Error(), "Tracker") {
		t.Fatalf("MissingHandles = %v, want an error naming Tracker", err)
	}
}
