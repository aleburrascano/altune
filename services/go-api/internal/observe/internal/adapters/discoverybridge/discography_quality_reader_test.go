package discoverybridge

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type stubQualityReader struct {
	cases      []discovery.DiscographyCase
	casesErr   error
	suspectErr error
}

func (s stubQualityReader) DiscographyQuality(context.Context, time.Time, discovery.DiscographyGroupBy, int) ([]discovery.DiscographyCase, error) {
	return s.cases, s.casesErr
}

func (s stubQualityReader) SuspectRate(context.Context, time.Time) (discovery.DiscographySuspectRate, error) {
	return discovery.DiscographySuspectRate{Rate: 0.5}, s.suspectErr
}

func query(r stubQualityReader) ([]DiscographyCaseDTO, error) {
	cases, _, _, err := QueryDiscographyQuality(context.Background(), r, time.Now(), ParseDiscographyGroupBy(""), 10)
	return cases, err
}

func errorCode(err error) string {
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) {
		return ""
	}
	return coded.ErrorCode()
}

func errorStatus(err error) int {
	var coded interface{ HTTPStatus() int }
	if !errors.As(err, &coded) {
		return 0
	}
	return coded.HTTPStatus()
}

func TestQueryDeadlineExceededMapsToTimeout(t *testing.T) {
	for name, r := range map[string]stubQualityReader{
		"cases":   {casesErr: context.DeadlineExceeded},
		"suspect": {suspectErr: context.DeadlineExceeded},
	} {
		if _, err := query(r); errorCode(err) != "observe.discography_quality_timeout" || errorStatus(err) != http.StatusGatewayTimeout {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestQueryOtherErrorsPassThrough(t *testing.T) {
	_, err := query(stubQualityReader{casesErr: context.Canceled})
	if !errors.Is(err, context.Canceled) || errorCode(err) != "" {
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
	cases, err := query(stubQualityReader{cases: []discovery.DiscographyCase{{ArtistRef: "a"}}})
	if err != nil || len(cases) != 1 {
		t.Fatalf("cases = %#v, err = %v", cases, err)
	}
	if cases[0].ProviderCounts == nil || len(cases[0].ProviderCounts) != 0 {
		t.Fatalf("ProviderCounts = %#v", cases[0].ProviderCounts)
	}
}
