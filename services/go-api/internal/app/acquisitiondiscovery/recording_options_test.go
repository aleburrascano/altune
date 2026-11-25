package acquisitiondiscovery_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/acquisitiontest"
	"altune/go-api/internal/app/acquisitiondiscovery"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/search"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

const (
	unfoundISRC     = "CAA509814003"
	searchOnlyMBID  = "5d6efd30-23c6-4a9b-85af-706ea2df9022"
	unfoundTitle    = "Drinking in L.A."
	unfoundArtist   = "Bran Van 3000"
	isrcLookup      = "RecordingsByISRC " + unfoundISRC
	rerecordingSeed = "RecordingDetail " + searchOnlyMBID
	catalogPin      = "b1a9c0e9-d987-4042-ae91-78d6a3267d69"
	pinLookup       = "RecordingByMBID " + catalogPin
)

type countingProvider struct {
	discoverytest.FakeProvider
	searches atomic.Int32
}

func (p *countingProvider) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	p.searches.Add(1)
	return p.FakeProvider.Search(ctx, query, kinds)
}

func searchFinding(t *testing.T, p *countingProvider) *search.Service {
	t.Helper()
	p.Provider = domain.ProviderDeezer
	p.Results = []domain.SearchResult{{
		Kind: domain.ResultKindTrack, Title: unfoundTitle, Subtitle: unfoundArtist,
		ISRC: unfoundISRC, RecordingMBID: searchOnlyMBID,
	}}
	return search.NewService(discoverytest.NewTracker(t), []discovery.SearchProvider{p}, discovery.NewCircuitBreaker())
}

type musicBrainzLog struct {
	mu    sync.Mutex
	calls []string
}

func (m *musicBrainzLog) record(call string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, call)
}

func (m *musicBrainzLog) asked() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

func (m *musicBrainzLog) RecordingsByISRC(_ context.Context, isrc string) ([]discovery.ISRCRecording, error) {
	m.record("RecordingsByISRC " + isrc)
	return nil, nil
}

func (m *musicBrainzLog) RecordingByMBID(_ context.Context, mbid string) (discovery.MBRecording, bool, error) {
	m.record("RecordingByMBID " + mbid)
	return discovery.MBRecording{}, false, nil
}

func (m *musicBrainzLog) WorkRecordings(_ context.Context, mbid string) ([]discovery.MBWorkRecording, error) {
	m.record("WorkRecordings " + mbid)
	return nil, nil
}

func (m *musicBrainzLog) RecordingDetail(_ context.Context, mbid string) (discovery.MBRecordingDetail, bool, error) {
	m.record("RecordingDetail " + mbid)
	return discovery.MBRecordingDetail{}, false, nil
}

func acquireUnfoundTrack(t *testing.T, disc *discovery.Discovery, catalogMBID *string) {
	t.Helper()
	isrc := unfoundISRC
	track := acquisition.TrackSnapshot{
		ID: shared.NewTrackId(), UserId: shared.NewUserId(uuid.New()),
		Title: unfoundTitle, Artist: unfoundArtist, ISRC: &isrc, CatalogMBID: catalogMBID, Status: acquisition.TrackPending,
	}
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
	svc := acquisition.NewAcquireTrackAudioService(ledger, acquisition.NewSourceRegistry(), nil, acquisitiondiscovery.RecordingOptions(disc)...)

	_ = svc.Execute(context.Background(), track.UserId, track.ID)

	got, err := ledger.Get(context.Background(), track.ID, track.UserId)
	if err != nil || got == nil || got.Status != acquisition.TrackFailed {
		t.Fatalf("after a run with no audio source the track is %+v, %v; want it failed", got, err)
	}
}

func TestRecordingOptions_WithSearchAndMusicBrainzTheRunAnchorsTheISRCAndLooksForARerecordingOfTheSearchHit(t *testing.T) {
	provider := &countingProvider{}
	musicBrainz := &musicBrainzLog{}

	acquireUnfoundTrack(t, &discovery.Discovery{Search: searchFinding(t, provider), Recordings: musicBrainz}, nil)

	if provider.searches.Load() == 0 {
		t.Error("discovery search was never asked, want the resolver to search for the track")
	}
	asked := musicBrainz.asked()
	if !slices.Contains(asked, isrcLookup) {
		t.Errorf("MusicBrainz was asked %q, want the ISRC authority asked for %s", asked, unfoundISRC)
	}
	if !slices.Contains(asked, rerecordingSeed) {
		t.Errorf("MusicBrainz was asked %q, want the rerecording finder to start from the search hit's %s", asked, searchOnlyMBID)
	}
}

func TestRecordingOptions_WithSearchAndMusicBrainzTheResolverLooksUpTheCatalogPin(t *testing.T) {
	musicBrainz := &musicBrainzLog{}
	pin := catalogPin

	acquireUnfoundTrack(t, &discovery.Discovery{Search: searchFinding(t, &countingProvider{}), Recordings: musicBrainz}, &pin)

	if asked := musicBrainz.asked(); !slices.Contains(asked, pinLookup) {
		t.Errorf("MusicBrainz was asked %q, want the resolver's recording lookup asked for the catalog pin %s", asked, catalogPin)
	}
}

func TestRecordingOptions_WithSearchAloneTheRunStillResolvesThroughSearch(t *testing.T) {
	provider := &countingProvider{}

	acquireUnfoundTrack(t, &discovery.Discovery{Search: searchFinding(t, provider)}, nil)

	if provider.searches.Load() == 0 {
		t.Error("discovery search was never asked, want the resolver wired without MusicBrainz")
	}
}

func TestRecordingOptions_WithoutSearchMusicBrainzIsNeverAsked(t *testing.T) {
	musicBrainz := &musicBrainzLog{}

	acquireUnfoundTrack(t, &discovery.Discovery{Recordings: musicBrainz}, nil)

	if asked := musicBrainz.asked(); len(asked) != 0 {
		t.Errorf("MusicBrainz was asked %q, want nothing: without discovery search there are no recording options", asked)
	}
}
