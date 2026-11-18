package goapi_test

import (
	"altune/overseer/internal/goapi"
	"errors"
	"testing"
)

func TestNullEventSourceReportsBucketError(t *testing.T) {
	down := errors.New("bucket down")
	n := goapi.NewNullEventSource(down)
	if n.Status() != goapi.StatusDown {
		t.Fatalf("status = %v, want down", n.Status())
	}
	if !errors.Is(n.LastError(), down) {
		t.Fatalf("LastError() = %v, want wrapping %v", n.LastError(), down)
	}
}
