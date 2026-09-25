package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestJobFailingCondition(t *testing.T) {
	ctx := context.Background()
	a := &App{}
	name := jobDeletedIdentityErasure
	jc := a.job(name)
	cond := buildJobCondition(name, jc)

	if cond.Key != "job_failing:deleted identity erasure" {
		t.Fatalf("key = %q", cond.Key)
	}

	for range jobFailureEscalation - 1 {
		jc.record(errors.New("boom secret"))
	}
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("fired below escalation: %+v", got)
	}

	jc.record(errors.New("boom secret"))
	got := cond.Eval(ctx)
	if got == nil {
		t.Fatal("consecutive failures produced no alert")
	}
	if strings.Contains(got.Message, "boom") || !strings.Contains(got.Message, string(name)) {
		t.Fatalf("message = %q, want job name and no error text", got.Message)
	}

	if _, ok := a.SetJobEnabled(name, false); !ok {
		t.Fatal("job not registered")
	}
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("disabled job fired: %+v", got)
	}
	a.SetJobEnabled(name, true)

	jc.record(nil)
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("recovered job still firing: %+v", got)
	}
}
