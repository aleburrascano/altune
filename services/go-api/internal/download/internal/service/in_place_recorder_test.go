package service_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeVerdicts struct {
	recorded bool
	err      error
	calls    int
}

func (f *fakeVerdicts) RecordInPlaceVerdict(context.Context, ids.TrackId, domain.InPlaceRecord) (bool, error) {
	f.calls++
	return f.recorded, f.err
}

func newInPlaceRecorder(t *testing.T, verdicts ports.InPlaceVerdicts, state ports.InPlaceState) *service.InPlaceRecorder {
	t.Helper()
	recorder, err := service.NewInPlaceRecorder(verdicts, state)
	if err != nil {
		t.Fatal(err)
	}
	return recorder
}

func TestInPlaceRecorder_AStateFailureAfterTheVerdictIsReturnedAndRetryRewritesTheSameVerdict(t *testing.T) {
	verdicts := &fakeVerdicts{recorded: true}
	state := memory.NewUpgradeState(time.Now)
	state.FailWith(errors.New("state unavailable"))
	recorder := newInPlaceRecorder(t, verdicts, state)
	id := ids.NewTrackId()
	record := domain.InPlaceRecord{AudioRef: "ref", Generation: 2, Verdict: "hard", Grade: "A"}

	recorded, err := recorder.RecordInPlace(context.Background(), id, record)
	if err == nil || recorded {
		t.Fatalf("RecordInPlace = %v, %v, want the state error and not recorded", recorded, err)
	}

	state.FailWith(nil)
	recorded, err = recorder.RecordInPlace(context.Background(), id, record)
	if err != nil || !recorded {
		t.Fatalf("retried RecordInPlace = %v, %v, want recorded", recorded, err)
	}
	if verdicts.calls != 2 || state.InPlaceStateWrites() != 2 {
		t.Errorf("verdict writes = %d, state writes = %d, want the idempotent pair twice", verdicts.calls, state.InPlaceStateWrites())
	}
}

func TestInPlaceRecorder_AVerdictOnAFileThatChangedWritesNoState(t *testing.T) {
	verdicts := &fakeVerdicts{recorded: false}
	state := memory.NewUpgradeState(time.Now)

	recorded, err := newInPlaceRecorder(t, verdicts, state).RecordInPlace(context.Background(), ids.NewTrackId(), domain.InPlaceRecord{AudioRef: "old"})

	if err != nil || recorded || state.InPlaceStateWrites() != 0 {
		t.Errorf("RecordInPlace = %v, %v, state writes = %d, want not recorded and no state write", recorded, err, state.InPlaceStateWrites())
	}
}

func TestInPlaceRecorder_AVerdictErrorWritesNoState(t *testing.T) {
	verdicts := &fakeVerdicts{err: errors.New("library down")}
	state := memory.NewUpgradeState(time.Now)

	_, err := newInPlaceRecorder(t, verdicts, state).RecordInPlace(context.Background(), ids.NewTrackId(), domain.InPlaceRecord{})

	if !errors.Is(err, verdicts.err) || state.InPlaceStateWrites() != 0 {
		t.Errorf("RecordInPlace = %v, state writes = %d, want the verdict error and no state write", err, state.InPlaceStateWrites())
	}
}

func TestNewInPlaceRecorderRefusesAMissingVerdictsOrState(t *testing.T) {
	cases := map[string]func() (*service.InPlaceRecorder, error){
		"no verdicts": func() (*service.InPlaceRecorder, error) {
			return service.NewInPlaceRecorder(nil, memory.NewUpgradeState(time.Now))
		},
		"no state": func() (*service.InPlaceRecorder, error) { return service.NewInPlaceRecorder(&fakeVerdicts{}, nil) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if recorder, err := build(); err == nil || recorder != nil {
				t.Errorf("NewInPlaceRecorder = %v, %v, want a refusal", recorder, err)
			}
		})
	}
}
