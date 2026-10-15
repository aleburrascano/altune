package ports

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
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
	got, err := collectBesideFailure(&acqdomain.SourceUnavailableError{Source: "yt", Err: errors.New("429")})

	if !acqdomain.IsSourceUnavailable(err) || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty and a source-unavailable error", got, err)
	}
}

func TestCollectCandidates_EmptyMergeBesidePlainFailureIsDegraded(t *testing.T) {
	parseErr := errors.New("parse")

	_, err := collectBesideFailure(parseErr)

	if !errors.Is(err, acqdomain.ErrSearchDegraded) || !errors.Is(err, parseErr) {
		t.Fatalf("err = %v, want ErrSearchDegraded wrapping the parse failure", err)
	}
}

func TestCollectCandidatesUntilEnough_KeepsCandidatesBesideAnUnavailableFailure(t *testing.T) {
	want := AudioCandidate{URL: "https://example.com/a"}
	run := func(i int) ([]AudioCandidate, error) {
		if i == 0 {
			return nil, &acqdomain.SourceUnavailableError{Source: "yt", Err: errors.New("429")}
		}
		return []AudioCandidate{want}, nil
	}

	got, outage, err := CollectCandidatesReportingOutage(2, unlimitedCandidates, run,
		func(int, []AudioCandidate) {}, func(int, error) {}, func(e error) error { return e })

	if err != nil || len(got) != 1 || got[0].URL != want.URL || !acqdomain.IsSourceUnavailable(outage) {
		t.Fatalf("got %v, outage %v, err %v; want the candidate and the outage", got, outage, err)
	}
}
