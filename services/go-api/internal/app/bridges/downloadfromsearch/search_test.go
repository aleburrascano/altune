package downloadfromsearch_test

import (
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/download"
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/service/query"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type recordingSearcher struct {
	out         *query.SearchOutput
	err         error
	query       *domain.SearchQuery
	user        ids.UserId
	saveHistory bool
}

func (s *recordingSearcher) Execute(_ context.Context, user ids.UserId, query *domain.SearchQuery, saveHistory bool) (*query.SearchOutput, error) {
	s.query, s.user, s.saveHistory = query, user, saveHistory
	return s.out, s.err
}

func searching(results ...domain.SearchResult) *recordingSearcher {
	return &recordingSearcher{out: &query.SearchOutput{Results: results}}
}

func TestSearchRecordings_AsksDiscoveryForTracksAloneAsNoUserAndSavesNoHistory(t *testing.T) {
	searcher := searching()

	if _, err := downloadfromsearch.NewSearch(searcher).SearchRecordings(context.Background(), "The Cure Boys Don't Cry", 10); err != nil {
		t.Fatalf("SearchRecordings: %v", err)
	}

	want := domain.SearchQuery{Raw: "The Cure Boys Don't Cry", Kinds: domain.ResultKindSet{domain.ResultKindTrack: true}, Limit: 10}
	if searcher.query == nil || !reflect.DeepEqual(*searcher.query, want) {
		t.Errorf("query = %+v, want %+v", searcher.query, want)
	}
	if searcher.user != (ids.UserId{}) || searcher.saveHistory {
		t.Errorf("user %v saveHistory %v, want no user and no history", searcher.user, searcher.saveHistory)
	}
}

func TestSearchRecordings_CarriesEveryFactOfATrackHit(t *testing.T) {
	searcher := searching(domain.SearchResult{
		Kind: domain.ResultKindTrack, Title: "Drinking in L.A.", Subtitle: "Bran Van 3000", ISRC: "CAA509814003",
		RecordingMBID: "remix-mbid", Duration: 236, Album: "Glee",
		Sources: []domain.ProviderLink{{Provider: domain.ProviderYouTube, ExternalID: "vid", URL: "https://youtu.be/vid"}},
	})

	got, err := downloadfromsearch.NewSearch(searcher).SearchRecordings(context.Background(), "q", 10)

	want := []download.SearchedRecording{{
		Title: "Drinking in L.A.", Artist: "Bran Van 3000", ISRC: "CAA509814003", MBID: "remix-mbid", Duration: 236,
		Sources: []download.ProviderRef{{Provider: download.ProviderYouTube, ExternalID: "vid", URL: "https://youtu.be/vid"}},
	}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("recordings %+v, err %v; want %+v", got, err, want)
	}
}

func TestSearchRecordings_KeepsOnlyTrackHits(t *testing.T) {
	searcher := searching(
		domain.SearchResult{Kind: domain.ResultKindAlbum, Title: "Album", ISRC: "CAA509814003"},
		domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Track"},
	)

	got, err := downloadfromsearch.NewSearch(searcher).SearchRecordings(context.Background(), "q", 10)

	if err != nil || len(got) != 1 || got[0].Title != "Track" {
		t.Errorf("recordings = %+v, err %v; want the track alone", got, err)
	}
}

func TestSearchRecordings_KeepsEveryDiscoveryProviderNameByteForByte(t *testing.T) {
	var sources []domain.ProviderLink
	for p := domain.ProviderUnknown; p <= domain.ProviderSpotify; p++ {
		sources = append(sources, domain.ProviderLink{Provider: p, ExternalID: "id-" + p.String()})
	}
	searcher := searching(domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Song", Sources: sources})

	got, err := downloadfromsearch.NewSearch(searcher).SearchRecordings(context.Background(), "q", 10)

	if err != nil || len(got) != 1 || len(got[0].Sources) != len(sources) {
		t.Fatalf("recordings %+v, err %v; want one source per provider", got, err)
	}
	for i, src := range sources {
		if got, want := string(got[0].Sources[i].Provider), src.Provider.String(); got != want {
			t.Errorf("source %d provider = %q, want %q", i, got, want)
		}
	}
}

func TestSearchRecordings_KeysSourcesByAcquisitionsProviderConstants(t *testing.T) {
	searcher := searching(domain.SearchResult{
		Kind: domain.ResultKindTrack, Title: "Song", Subtitle: "Artist",
		Sources: []domain.ProviderLink{
			{Provider: domain.ProviderYouTube, ExternalID: "vid123"},
			{Provider: domain.ProviderDeezer, ExternalID: "3135556"},
			{Provider: domain.ProviderSoundCloud, ExternalID: "999"},
		},
	})
	resolver := download.NewRecordingResolver(downloadfromsearch.NewSearch(searcher))

	identity, err := resolver.Resolve(context.Background(), download.RecordingQuery{Title: "Song", Artist: "Artist"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for key, wantID := range map[download.RecordingProvider]string{
		download.ProviderYouTube:    "vid123",
		download.ProviderDeezer:     "3135556",
		download.ProviderSoundCloud: "999",
	} {
		got, ok := identity.SourceFor(key)
		if !ok || got.ExternalID != wantID {
			t.Errorf("SourceFor(%q) = %+v, %v; want ExternalID %q", key, got, ok, wantID)
		}
	}
}

func TestSearchRecordings_PassesASearchFailureThrough(t *testing.T) {
	boom := errors.New("search down")

	got, err := downloadfromsearch.NewSearch(&recordingSearcher{err: boom}).SearchRecordings(context.Background(), "q", 10)

	if !errors.Is(err, boom) || got != nil {
		t.Errorf("recordings %+v, err %v; want the search failure", got, err)
	}
}

func TestSearchRecordings_NoOutputIsNoRecordings(t *testing.T) {
	got, err := downloadfromsearch.NewSearch(&recordingSearcher{}).SearchRecordings(context.Background(), "q", 10)

	if err != nil || got != nil {
		t.Errorf("recordings %+v, err %v; want none", got, err)
	}
}

func TestSearchRecordings_RefusesAQueryDiscoveryCannotRunWithoutAskingIt(t *testing.T) {
	searcher := searching()

	_, err := downloadfromsearch.NewSearch(searcher).SearchRecordings(context.Background(), "", 10)

	if err == nil || !strings.Contains(err.Error(), "build resolve query") || searcher.query != nil {
		t.Errorf("err %v, asked %+v; want the query refused before discovery runs it", err, searcher.query)
	}
}
