package discoverybridge

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/observe/domain"
	"context"
	"errors"
	"testing"
	"time"
)

type stubQualityReader struct {
	cases      []ports.DiscographyCase
	casesErr   error
	suspectErr error
}

func (s stubQualityReader) DiscographyQuality(context.Context, time.Time, ports.DiscographyGroupBy, int) ([]ports.DiscographyCase, error) {
	return s.cases, s.casesErr
}

func (s stubQualityReader) SuspectRate(context.Context, time.Time) (ports.DiscographySuspectRate, error) {
	return ports.DiscographySuspectRate{Rate: 0.5}, s.suspectErr
}

func query(r stubQualityReader) ([]DiscographyCaseDTO, error) {
	cases, _, _, err := QueryDiscographyQuality(context.Background(), r, time.Now(), ParseDiscographyGroupBy(""), 10)
	return cases, err
}

func TestQueryDeadlineExceededMapsToTimeout(t *testing.T) {
	for name, r := range map[string]stubQualityReader{
		"cases":   {casesErr: context.DeadlineExceeded},
		"suspect": {suspectErr: context.DeadlineExceeded},
	} {
		if _, err := query(r); !errors.Is(err, domain.ErrDiscographyQualityTimeout) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestQueryOtherErrorsPassThrough(t *testing.T) {
	_, err := query(stubQualityReader{casesErr: context.Canceled})
	if !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrDiscographyQualityTimeout) {
		t.Fatalf("err = %v", err)
	}
}

func TestQueryZeroCasesGivesNonNilSlice(t *testing.T) {
	cases, err := query(stubQualityReader{})
	if err != nil || cases == nil || len(cases) != 0 {
		t.Fatalf("cases = %#v, err = %v", cases, err)
	}
}

func TestQueryNilProviderCountsBecomesEmptyMap(t *testing.T) {
	cases, err := query(stubQualityReader{cases: []ports.DiscographyCase{{ArtistRef: "a"}}})
	if err != nil || len(cases) != 1 {
		t.Fatalf("cases = %#v, err = %v", cases, err)
	}
	if cases[0].ProviderCounts == nil || len(cases[0].ProviderCounts) != 0 {
		t.Fatalf("ProviderCounts = %#v", cases[0].ProviderCounts)
	}
}
