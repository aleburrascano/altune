package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"testing"
)

func throttledSource() *stubSource {
	return &stubSource{
		name:    "ytdlp",
		findErr: &ports.SourceUnavailableError{Source: "ytdlp", Err: errors.New("HTTP Error 429")},
	}
}

func searchFailureCode(t *testing.T, reg *SourceRegistry) domain.FailureCode {
	t.Helper()
	_, err := NewSearchStep(reg).Execute(context.Background(), &AcquisitionContext{}, pipelineStart{})
	if err == nil {
		t.Fatal("search step succeeded, want a failure")
	}
	return failureCode(&StepError{Step: stepNameSearch, Err: err})
}

func TestSourceRegistry_FindEmptyWithUnavailableSourceReportsUnavailable(t *testing.T) {
	reg := NewSourceRegistry(throttledSource(), &stubSource{name: "streamrip"})

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if !ports.IsSourceUnavailable(err) || len(got) != 0 {
		t.Fatalf("Find = %v, %v; want empty and a source-unavailable error", got, err)
	}
	if code := searchFailureCode(t, reg); code != domain.FailureSourceUnavailable {
		t.Fatalf("failure code = %s, want %s", code, domain.FailureSourceUnavailable)
	}
}

func TestSourceRegistry_FindPartialFailureWithCandidatesSucceeds(t *testing.T) {
	reg := NewSourceRegistry(
		throttledSource(),
		&stubSource{name: "streamrip", found: []ports.AudioCandidate{candidate("u1")}},
	)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %v, %v; want one candidate and nil error", got, err)
	}
}

func TestSourceRegistry_FindAllEmptyWithoutErrorIsNoMatch(t *testing.T) {
	reg := NewSourceRegistry(&stubSource{name: "a"}, &stubSource{name: "b"})

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Find = %v, %v; want empty and nil error", got, err)
	}
	if code := searchFailureCode(t, reg); code != domain.FailureNoMatchFound {
		t.Fatalf("failure code = %s, want %s", code, domain.FailureNoMatchFound)
	}
}
