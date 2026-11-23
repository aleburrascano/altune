package observediscovery_test

import (
	"altune/go-api/internal/app/observediscovery"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNewEvalMeterRunner_NoEvalRunIsNoRunner(t *testing.T) {
	if run := observediscovery.NewEvalMeterRunner(nil); run != nil {
		t.Error("runner is non-nil without an eval run, want nil so the meter treats it as unset")
	}
}

func TestNewEvalMeterRunner_AFailedRunPassesItsErrorWithAZeroResult(t *testing.T) {
	wantErr := errors.New("eval runner boom")
	run := observediscovery.NewEvalMeterRunner(func(context.Context) (discovery.SmokeResult, error) {
		return discovery.SmokeResult{Score: 1}, wantErr
	})

	res, err := run(context.Background())

	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(res, observe.EvalMeterResult{}) {
		t.Errorf("result = %+v, want the zero Result", res)
	}
}

func TestNewEvalMeterRunner_CarriesEveryScorecardField(t *testing.T) {
	run := observediscovery.NewEvalMeterRunner(func(context.Context) (discovery.SmokeResult, error) {
		return discovery.SmokeResult{
			Score:     0.75,
			Baseline:  0.9,
			Regressed: true,
			Errored:   2,
			Queries: []discovery.SmokeQueryResult{
				{Query: "radiohead", Expect: "Radiohead", Passed: true, Position: 1},
				{Query: "bjork", Expect: "Björk", Passed: false, Position: 7},
			},
		}, nil
	})

	got, err := run(context.Background())

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := observe.EvalMeterResult{
		Score:     0.75,
		Baseline:  0.9,
		Regressed: true,
		Errored:   2,
		Queries: []observe.EvalMeterQueryResult{
			{Query: "radiohead", Expect: "Radiohead", Passed: true, Position: 1},
			{Query: "bjork", Expect: "Björk", Passed: false, Position: 7},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("result = %+v, want %+v", got, want)
	}
}
