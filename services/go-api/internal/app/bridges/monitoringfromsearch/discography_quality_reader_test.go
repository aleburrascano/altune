package monitoringfromsearch_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromsearch"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type recordingQualityStore struct {
	calls      int
	gotSince   time.Time
	gotGroupBy searchtest.DiscographyGroupBy
	gotLimit   int
	cases      []searchtest.DiscographyCase
	casesErr   error
	rate       searchtest.DiscographySuspectRate
	rateErr    error
}

func (s *recordingQualityStore) DiscographyQuality(_ context.Context, since time.Time, groupBy searchtest.DiscographyGroupBy, limit int) ([]searchtest.DiscographyCase, error) {
	s.calls++
	s.gotSince, s.gotGroupBy, s.gotLimit = since, groupBy, limit
	return s.cases, s.casesErr
}

func (s *recordingQualityStore) SuspectRate(_ context.Context, since time.Time) (searchtest.DiscographySuspectRate, error) {
	s.gotSince = since
	return s.rate, s.rateErr
}

func TestDiscographyQualityReader_AsksTheStoreForTheSameWindowGroupingAndLimit(t *testing.T) {
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, groupBy := range []monitoring.DiscographyGroupBy{monitoring.GroupByArtist, monitoring.GroupByProvider, monitoring.GroupByContaminationBand} {
		t.Run(string(groupBy), func(t *testing.T) {
			store := &recordingQualityStore{}

			if _, err := monitoringfromsearch.NewDiscographyQualityReader(store).DiscographyQuality(t.Context(), since, groupBy, 200); err != nil {
				t.Fatalf("err = %v", err)
			}

			if !store.gotSince.Equal(since) || string(store.gotGroupBy) != string(groupBy) || store.gotLimit != 200 {
				t.Fatalf("store asked since %v by %q limit %d, want %v by %q limit 200", store.gotSince, store.gotGroupBy, store.gotLimit, since, groupBy)
			}
		})
	}
}

func TestDiscographyQualityReader_CarriesEveryCaseField(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	store := &recordingQualityStore{cases: []searchtest.DiscographyCase{
		{ArtistRef: "spotify:a", Releases: 4, SingleProvider: 1, SingleProviderNoID: 2, ProviderCounts: map[string]int{"spotify": 4}, LastSeen: seen},
		{ArtistRef: "spotify:b"},
	}}

	got, err := monitoringfromsearch.NewDiscographyQualityReader(store).DiscographyQuality(t.Context(), seen, monitoring.GroupByArtist, 10)

	want := []monitoring.DiscographyCase{
		{ArtistRef: "spotify:a", Releases: 4, SingleProvider: 1, SingleProviderNoID: 2, ProviderCounts: map[string]int{"spotify": 4}, LastSeen: seen},
		{ArtistRef: "spotify:b"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cases = %+v err %v, want %+v", got, err, want)
	}
}

func TestDiscographyQualityReader_AGroupingDiscoveryLacksFailsWithoutAskingTheStore(t *testing.T) {
	store := &recordingQualityStore{}

	_, err := monitoringfromsearch.NewDiscographyQualityReader(store).DiscographyQuality(t.Context(), time.Now(), monitoring.DiscographyGroupBy("genre"), 10)

	if err == nil || store.calls != 0 {
		t.Fatalf("err = %v after %d store calls, want an error before any call", err, store.calls)
	}
}

func TestDiscographyQualityReader_StoreErrorsKeepTheirIdentity(t *testing.T) {
	reader := monitoringfromsearch.NewDiscographyQualityReader(&recordingQualityStore{casesErr: context.DeadlineExceeded, rateErr: context.DeadlineExceeded})

	cases, casesErr := reader.DiscographyQuality(t.Context(), time.Now(), monitoring.GroupByArtist, 10)
	_, rateErr := reader.SuspectRate(t.Context(), time.Now())

	if cases != nil || !errors.Is(casesErr, context.DeadlineExceeded) || !errors.Is(rateErr, context.DeadlineExceeded) {
		t.Fatalf("cases %v err %v / %v, want no cases and the store's deadline errors", cases, casesErr, rateErr)
	}
}

func TestDiscographyQualityReader_SuspectRateCarriesTheStoresRate(t *testing.T) {
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	sample := since.Add(time.Hour)
	store := &recordingQualityStore{rate: searchtest.DiscographySuspectRate{Rate: 0.25, LastSample: sample}}

	got, err := monitoringfromsearch.NewDiscographyQualityReader(store).SuspectRate(t.Context(), since)

	if err != nil || got != (monitoring.DiscographySuspectRate{Rate: 0.25, LastSample: sample}) || !store.gotSince.Equal(since) {
		t.Fatalf("rate = %+v err %v since %v, want 0.25 at %v for %v", got, err, store.gotSince, sample, since)
	}
}

func TestDiscographyQualityReader_SuspectRateErrorGivesAZeroRate(t *testing.T) {
	store := &recordingQualityStore{rate: searchtest.DiscographySuspectRate{Rate: 0.25}, rateErr: errors.New("boom")}

	got, err := monitoringfromsearch.NewDiscographyQualityReader(store).SuspectRate(t.Context(), time.Now())

	if err == nil || got != (monitoring.DiscographySuspectRate{}) {
		t.Fatalf("rate = %+v err %v, want a zero rate and the error", got, err)
	}
}
