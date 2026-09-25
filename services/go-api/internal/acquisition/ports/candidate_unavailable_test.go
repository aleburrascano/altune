package ports

import (
	"errors"
	"testing"
)

func collectBesideFailure(failure error) ([]AudioCandidate, error) {
	run := func(i int) ([]AudioCandidate, error) {
		if i == 0 {
			return nil, failure
		}
		return nil, nil
	}
	return CollectCandidates(2, run,
		func(int, []AudioCandidate) {},
		func(int, error) {},
		func(e error) error { return e })
}

func TestCollectCandidates_EmptyMergeBesideUnavailableFailureIsUnavailable(t *testing.T) {
	got, err := collectBesideFailure(&SourceUnavailableError{Source: "yt", Err: errors.New("429")})

	if !IsSourceUnavailable(err) || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty and a source-unavailable error", got, err)
	}
}

func TestCollectCandidates_EmptyMergeBesidePlainFailureIsNotAnError(t *testing.T) {
	if _, err := collectBesideFailure(errors.New("parse")); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}
