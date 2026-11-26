package ports_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/domain"
	"context"
	"errors"
	"fmt"
	"testing"
)

type stubSource struct {
	name       download.SourceName
	candidates []download.AudioCandidate
	err        error
}

func (s stubSource) Name() download.SourceName { return s.name }

func (s stubSource) Find(context.Context, download.FindRequest) ([]download.AudioCandidate, error) {
	return s.candidates, s.err
}

func (s stubSource) Fetch(context.Context, download.AudioCandidate, string) (download.FetchedAudio, error) {
	return download.FetchedAudio{}, errors.New("stubSource does not fetch")
}

func unavailable() error {
	return &domain.SourceUnavailableError{Source: "yt", Err: errors.New("429")}
}

func isSourceUnavailable(err error) bool {
	var u *domain.SourceUnavailableError
	return errors.As(err, &u)
}

func findBesideFailure(failure error) ([]download.AudioCandidate, error) {
	return download.NewSourceRegistry(stubSource{name: "a", err: failure}, stubSource{name: "b"}).
		Find(context.Background(), download.FindRequest{Title: "t", Artist: "a"})
}

func TestFind_EmptyMergeBesideUnavailableFailureIsUnavailable(t *testing.T) {
	got, err := findBesideFailure(unavailable())

	if !isSourceUnavailable(err) || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty and a source-unavailable error", got, err)
	}
}

func TestFind_EmptyMergeBesidePlainFailureIsDegraded(t *testing.T) {
	parseErr := errors.New("parse")

	_, err := findBesideFailure(parseErr)

	if !errors.Is(err, domain.ErrSearchDegraded) || !errors.Is(err, parseErr) {
		t.Fatalf("err = %v, want ErrSearchDegraded wrapping the parse failure", err)
	}
}

func TestFindReportingOutage_KeepsCandidatesBesideAnUnavailableFailure(t *testing.T) {
	want := download.AudioCandidate{URL: "https://example.com/a"}
	registry := download.NewSourceRegistry(
		stubSource{name: "a", err: unavailable()},
		stubSource{name: "b", candidates: []download.AudioCandidate{want}})

	got, err := registry.FindReportingOutage(context.Background(), download.FindRequest{Title: "t", Artist: "a"})

	if err != nil || len(got.Candidates) != 1 || got.Candidates[0].URL != want.URL || !isSourceUnavailable(got.Outage) {
		t.Fatalf("got %+v, err %v; want the candidate and the outage", got, err)
	}
}

func TestFindReportingOutage_MergesEverySource(t *testing.T) {
	sources := make([]download.AudioSource, 0, 3)
	for i := range 3 {
		out := make([]download.AudioCandidate, 0, 5)
		for j := range 5 {
			out = append(out, download.AudioCandidate{URL: fmt.Sprintf("https://example.com/%d/%d", i, j)})
		}
		sources = append(sources, stubSource{name: download.SourceName(fmt.Sprint(i)), candidates: out})
	}

	got, err := download.NewSourceRegistry(sources...).FindReportingOutage(context.Background(), download.FindRequest{Title: "t", Artist: "a"})

	if err != nil || got.Outage != nil || len(got.Candidates) != 15 {
		t.Fatalf("got %d candidates, outage %v, err %v; want all 15 from 3 sources", len(got.Candidates), got.Outage, err)
	}
}
