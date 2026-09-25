package guard_test

import (
	"strings"
	"testing"
)

func TestHistoryImportsNoConcreteBucket(t *testing.T) {
	for _, dep := range deps(t, "altune/overseer/internal/history") {
		if strings.Contains(dep, "altune/overseer/internal/buckets/") {
			t.Errorf("internal/history must not depend on a concrete bucket, but imports %s", dep)
		}
	}
}
