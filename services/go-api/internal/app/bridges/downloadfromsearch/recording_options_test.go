package downloadfromsearch_test

import (
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	searchtest.FakeProvider
	searches atomic.Int32
}

func (p *countingProvider) Search(ctx context.Context, query string, kinds map[search.ResultKind]bool) ([]search.SearchResult, error) {
	p.searches.Add(1)
	return p.FakeProvider.Search(ctx, query, kinds)
}

func searchFinding(t *testing.T, p *countingProvider) *search.SearchService {
	t.Helper()
	p.Provider = searchtest.ProviderDeezer
	p.Results = []search.SearchResult{searchtest.NewResult(
		search.ResultKindTrack,
		unfoundTitle,
		searchtest.WithSubtitle(unfoundArtist),
		searchtest.WithISRCCode(unfoundISRC),
		searchtest.WithRecordingMBID(searchOnlyMBID),
	)}
	return searchtest.NewSearchService(t, searchtest.NewTracker(t), []searchtest.SearchProvider{p}, searchtest.NewCircuitBreaker(clock.Now))
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

func (m *musicBrainzLog) RecordingsByISRC(_ context.Context, isrc string) ([]search.ISRCRecording, error) {
	m.record("RecordingsByISRC " + isrc)
	return nil, nil
}

func (m *musicBrainzLog) RecordingByMBID(_ context.Context, mbid string) (search.MusicBrainzRecording, bool, error) {
	m.record("RecordingByMBID " + mbid)
	return search.MusicBrainzRecording{}, false, nil
}

func (m *musicBrainzLog) WorkRecordings(_ context.Context, mbid string) ([]search.MusicBrainzWorkRecording, error) {
	m.record("WorkRecordings " + mbid)
	return nil, nil
}

func (m *musicBrainzLog) RecordingDetail(_ context.Context, mbid string) (search.MusicBrainzRecordingDetail, bool, error) {
	m.record("RecordingDetail " + mbid)
	return search.MusicBrainzRecordingDetail{}, false, nil
}

func acquireUnfoundTrack(t *testing.T, disc *search.Module, catalogMBID *string) {
	t.Helper()
	isrc := unfoundISRC
	track := download.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: unfoundTitle, Artist: unfoundArtist, ISRC: &isrc, CatalogMBID: catalogMBID, Status: download.TrackPending,
	}
	ledger := downloadtest.NewLedger(downloadtest.LedgerTrack{TrackSnapshot: track})
	recording, err := downloadfromsearch.RecordingOptions(disc)
	if err != nil {
		t.Fatalf("RecordingOptions: %v", err)
	}
	svc := download.MustNewAcquireTrackAudioService(ledger, download.NewSourceRegistry(), discardingStore{}, download.NewTempWorkspace(),
		download.VerificationPorts{
			Identifier:      download.NoopAudioIdentifier(),
			RecordingLookup: recording.Lookup,
			Rerecordings:    recording.Rerecordings,
			Previews:        download.NoopPreviewMatcher(),
			Recordings:      recording.Resolver,
		}, download.StagePorts{
			Prober:        downloadtest.SilentAudioProber(),
			Tagger:        download.NoopAudioTagger(),
			Rejections:    download.NoopRejectionStore(),
			StoredAudio:   download.NoopStoredAudioReader(),
			Orphans:       download.MustDirectDeleteOrphans(deletedNothing{}),
			Skips:         download.NoopVerifySkipRecorder(),
			Events:        events.NoopPublisher(),
			DownloadLimit: download.Unlimited(),
			Observer:      download.NoObserver(),
			Sleep:         clock.Sleep,
			Now:           time.Now,
			Unit:          random.Real,
		})

	_ = svc.Execute(context.Background(), download.AcquisitionJob{UserID: track.UserId, TrackID: track.ID})

	got, err := ledger.Get(context.Background(), track.ID, track.UserId)
	if err != nil || got == nil || got.Status != download.TrackFailed {
		t.Fatalf("after a run with no audio source the track is %+v, %v; want it failed", got, err)
	}
}

type deletedNothing struct{}

func (deletedNothing) Delete(context.Context, string) error { return nil }

func TestRecordingOptions_WithSearchAndMusicBrainzTheRunAnchorsTheISRCAndLooksForARerecordingOfTheSearchHit(t *testing.T) {
	provider := &countingProvider{}
	musicBrainz := &musicBrainzLog{}

	acquireUnfoundTrack(t, &search.Module{Search: searchFinding(t, provider), Recordings: musicBrainz}, nil)

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

	acquireUnfoundTrack(t, &search.Module{Search: searchFinding(t, &countingProvider{}), Recordings: musicBrainz}, &pin)

	if asked := musicBrainz.asked(); !slices.Contains(asked, pinLookup) {
		t.Errorf("MusicBrainz was asked %q, want the resolver's recording lookup asked for the catalog pin %s", asked, catalogPin)
	}
}

func TestRecordingOptions_WithSearchAloneTheRunStillResolvesThroughSearch(t *testing.T) {
	provider := &countingProvider{}

	acquireUnfoundTrack(t, &search.Module{Search: searchFinding(t, provider)}, nil)

	if provider.searches.Load() == 0 {
		t.Error("discovery search was never asked, want the resolver wired without MusicBrainz")
	}
}

func TestRecordingOptions_WithoutSearchMusicBrainzIsNeverAsked(t *testing.T) {
	musicBrainz := &musicBrainzLog{}

	acquireUnfoundTrack(t, &search.Module{Recordings: musicBrainz}, nil)

	if asked := musicBrainz.asked(); len(asked) != 0 {
		t.Errorf("MusicBrainz was asked %q, want nothing: without discovery search there are no recording options", asked)
	}
}

type discardingStore struct{}

func (discardingStore) Exists(context.Context, string) (bool, error) { return false, nil }
func (discardingStore) Store(context.Context, string, string) error  { return nil }
func (discardingStore) Delete(context.Context, string) error         { return nil }
