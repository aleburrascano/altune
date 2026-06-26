package handler

import (
	"context"
	"errors"
	"testing"
)

func TestEvalMeter_DisabledState(t *testing.T) {
	m := NewEvalMeter(false, 0, nil)
	if st := m.Status(); st.State != "disabled" || st.Enabled {
		t.Fatalf("status = %+v, want disabled/!enabled", st)
	}
}

func TestEvalMeter_NoDataBeforeFirstRun(t *testing.T) {
	m := NewEvalMeter(true, 0, func(context.Context) (EvalResult, error) {
		return EvalResult{}, nil
	})
	if st := m.Status(); st.State != "no_data" {
		t.Fatalf("state = %q, want no_data before any run", st.State)
	}
}

func TestEvalMeter_OkAndRegression(t *testing.T) {
	m := NewEvalMeter(true, 0, func(context.Context) (EvalResult, error) {
		return EvalResult{Score: 0.81, Baseline: 0.80, Regressed: false}, nil
	})
	m.runOnce(context.Background())
	st := m.Status()
	if st.State != "ok" || st.Score == nil || *st.Score != 0.81 {
		t.Fatalf("status = %+v, want ok with score 0.81", st)
	}

	m2 := NewEvalMeter(true, 0, func(context.Context) (EvalResult, error) {
		return EvalResult{Score: 0.70, Baseline: 0.80, Regressed: true}, nil
	})
	m2.runOnce(context.Background())
	if st := m2.Status(); st.State != "regression" {
		t.Fatalf("state = %q, want regression", st.State)
	}
}

func TestEvalMeter_ErrorState(t *testing.T) {
	m := NewEvalMeter(true, 0, func(context.Context) (EvalResult, error) {
		return EvalResult{}, errors.New("provider unreachable")
	})
	m.runOnce(context.Background())
	if st := m.Status(); st.State != "error" || st.Error == "" {
		t.Fatalf("status = %+v, want error state", st)
	}
}

func TestEvalMeter_SkipIfRunning(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	m := NewEvalMeter(true, 0, func(context.Context) (EvalResult, error) {
		calls++
		close(started)
		<-release
		return EvalResult{}, nil
	})

	go m.runOnce(context.Background())
	<-started
	m.runOnce(context.Background()) // must skip while the first is in flight
	close(release)

	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1 (second skipped)", calls)
	}
}
