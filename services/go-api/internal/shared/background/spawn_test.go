package background_test

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"testing"
)

func TestGo_RunsFnAndClosesDoneWhenItReturns(t *testing.T) {
	ran := false

	<-background.Go(context.Background(), "test.go_ran", func() { ran = true })

	if !ran {
		t.Error("fn did not run before done closed")
	}
}

func TestGo_APanicInFnIsLoggedWithTheCallersRequestIDAndDoesNotCrash(t *testing.T) {
	ring := captureDetachedLogs(t)
	ctx := logging.ContextWithCorrelationID(context.Background(), "corr-go-1")

	<-background.Go(ctx, "test.go_panic", func() { panic("worker exploded") })

	rec := onlyRecord(t, ring, "test.go_panic")
	if rec.Attrs["corr_id"] != "corr-go-1" {
		t.Errorf("corr_id = %q, want the request id the caller passed", rec.Attrs["corr_id"])
	}
}
