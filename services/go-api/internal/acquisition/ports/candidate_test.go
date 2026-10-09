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

func TestCollectCandidatesUntilEnough_KeepsCandidatesBesideAnUnavailableFailure(t *testing.T) {
	want := AudioCandidate{URL: "https://example.com/a"}
	run := func(i int) ([]AudioCandidate, error) {
		if i == 0 {
			return nil, &SourceUnavailableError{Source: "yt", Err: errors.New("429")}
		}
		return []AudioCandidate{want}, nil
	}

	got, outage, err := CollectCandidatesReportingOutage(2, unlimitedCandidates, run,
		func(int, []AudioCandidate) {}, func(int, error) {}, func(e error) error { return e })

	if err != nil || len(got) != 1 || got[0].URL != want.URL || !IsSourceUnavailable(outage) {
		t.Fatalf("got %v, outage %v, err %v; want the candidate and the outage", got, outage, err)
	}
}
