package service_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func catalogAlbum(provider search.ProviderName, id, title string) search.SearchResult {
	return memory.NewResult(search.ResultKindAlbum, title,
		memory.WithSubtitle("Che"),
		memory.WithSource(provider, id, ""),
		memory.WithExtras(map[string]any{}))
}

type artistRecordingEventStore struct {
	events chan search.InteractionEvent
}

func newRecordingEventStore() *artistRecordingEventStore {
	return &artistRecordingEventStore{events: make(chan search.InteractionEvent, 8)}
}

func (s *artistRecordingEventStore) Append(_ context.Context, e search.InteractionEvent) error {
	s.events <- e
	return nil
}

func (s *artistRecordingEventStore) next(t *testing.T) search.InteractionEvent {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event recorded within 5s")
		return search.InteractionEvent{}
	}
}

func albumsFrom(albums map[search.ProviderName][]search.SearchResult) map[search.ProviderName]ports.ArtistContentProvider {
	providers := make(map[search.ProviderName]ports.ArtistContentProvider, len(albums))
	for name, served := range albums {
		providers[name] = artistContentFuncs{
			albums: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
				return served, nil
			},
		}
	}
	return providers
}

func observeDiscography(t *testing.T, albums map[search.ProviderName][]search.SearchResult) search.InteractionEvent {
	t.Helper()
	bg := searchtest.NewTracker(t)
	known := map[search.ProviderName]string{}
	for name := range albums {
		known[name] = name.String() + "-che"
	}
	store := newRecordingEventStore()
	svc := searchtest.NewArtistContent(t, albumsFrom(albums), searchtest.ArtistEvents(store, bg))
	if _, err := svc.GetAlbums(context.Background(), search.ProviderSpotify, "abc123", known, 50); err != nil {
		t.Fatalf("GetAlbums: %v", err)
	}
	return store.next(t)
}

func TestNoDiscographyObserver_LeavesTheAlbumsAnswerUnchanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) { noDiscographyObserverLeavesTheAlbumsAnswerUnchanged(t) })
}

func noDiscographyObserverLeavesTheAlbumsAnswerUnchanged(t *testing.T) {
	albums := map[search.ProviderName][]search.SearchResult{
		search.ProviderSpotify: {
			catalogAlbum(search.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(search.ProviderSpotify, "sp-b", "Sundown"),
		},
		domain.ProviderMusicBrainz: {catalogAlbum(domain.ProviderMusicBrainz, "mb-a", "Fully Loaded")},
	}
	known := map[search.ProviderName]string{search.ProviderSpotify: "sp-che", domain.ProviderMusicBrainz: "mb-che"}
	fetch := func(svc *service.GetArtistContentService) []string {
		resp, err := svc.GetAlbums(context.Background(), search.ProviderSpotify, "abc123", known, 50)
		if err != nil {
			t.Fatalf("GetAlbums: %v", err)
		}
		titles := make([]string, 0, len(resp.Items))
		for _, item := range resp.Items {
			titles = append(titles, item.Title)
		}
		return titles
	}
	store := newRecordingEventStore()

	observed := fetch(searchtest.NewArtistContent(t, albumsFrom(albums), searchtest.ArtistEvents(store, searchtest.NewTracker(t))))
	store.next(t)
	silent, err := service.NewGetArtistContentService(albumsFrom(albums), map[search.ProviderName]ports.AlbumContentProvider{}, service.SeedPathIdentity{}, service.NoAnchoring{}, service.NoDiscographyObserver{}, search.NewCircuitBreaker(clock.Now))
	if err != nil {
		t.Fatalf("NewGetArtistContentService: %v", err)
	}

	if got := fetch(silent); !slices.Equal(got, observed) {
		t.Errorf("titles with NoDiscographyObserver = %v, want %v as with telemetry", got, observed)
	}
	synctest.Wait()
	select {
	case ev := <-store.events:
		t.Errorf("event %+v recorded, want none beyond the telemetry service's own", ev)
	default:
	}
}

func TestDiscographyEmit_PayloadShapeAndNoUserID(t *testing.T) {
	ev := observeDiscography(t, map[search.ProviderName][]search.SearchResult{
		search.ProviderSpotify: {
			catalogAlbum(search.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(search.ProviderSpotify, "sp-b", "Sundown"),
		},
		domain.ProviderMusicBrainz: {
			catalogAlbum(domain.ProviderMusicBrainz, "mb-a", "Fully Loaded"),
		},
	})

	if ev.Type != domain.InteractionTypeDiscographyObserved {
		t.Fatalf("event type = %v, want discography_observed", ev.Type)
	}
	if !ev.UserId.IsSystem() {
		t.Errorf("event user id = %s, want the synthetic system identity", ev.UserId)
	}

	assertPayloadInt(t, ev.Payload, "releases", 2)
	assertPayloadInt(t, ev.Payload, "single_provider", 1)
	assertPayloadInt(t, ev.Payload, "single_provider_no_id", 0)
	if ref, _ := ev.Payload["artist_ref"].(string); ref != "spotify:abc123" {
		t.Errorf("artist_ref = %q, want spotify:abc123", ref)
	}
	if ev.OccurredAt.IsZero() {
		t.Error("event OccurredAt is zero, want the observation time")
	}
	if _, present := ev.Payload["last_seen"]; present {
		t.Error("payload carries dead last_seen; occurred_at is the source of truth")
	}
	counts, ok := ev.Payload["provider_counts"].(map[string]int)
	if !ok {
		t.Fatalf("provider_counts type = %T, want map[string]int", ev.Payload["provider_counts"])
	}
	if counts["spotify"] != 2 || counts["musicbrainz"] != 1 {
		t.Errorf("provider_counts = %v, want spotify:2 musicbrainz:1", counts)
	}

	for _, k := range []string{"user_id", "userId", "user", "uid", "user_hash", "hashed_user_id"} {
		if _, present := ev.Payload[k]; present {
			t.Errorf("payload leaks user identity via key %q: %v", k, ev.Payload[k])
		}
	}
	if len(ev.Payload) != 5 {
		t.Errorf("payload has %d keys, want exactly the 5 pinned fields: %v", len(ev.Payload), ev.Payload)
	}
}

func TestDiscographyEmit_ReleasesFromTheIdentityFanOutAreNeverSuspects(t *testing.T) {
	ev := observeDiscography(t, map[search.ProviderName][]search.SearchResult{
		search.ProviderSpotify: {
			catalogAlbum(search.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(search.ProviderSpotify, "sp-c", "Sundown"),
		},
		search.ProviderDeezer: {
			catalogAlbum(search.ProviderDeezer, "dz-b", "Closer to God"),
		},
		domain.ProviderMusicBrainz: {
			catalogAlbum(domain.ProviderMusicBrainz, "mb-c", "Sundown"),
		},
	})

	assertPayloadInt(t, ev.Payload, "releases", 3)
	assertPayloadInt(t, ev.Payload, "single_provider", 2)
	assertPayloadInt(t, ev.Payload, "single_provider_no_id", 0)
}

func discographyEmitLogs(t *testing.T, appendErr error) string {
	t.Helper()
	logs := searchtest.CaptureLogs(t)
	bg := searchtest.NewTracker(t)
	store := &searchtest.FakeEventStore{Err: appendErr}
	albums := map[search.ProviderName][]search.SearchResult{
		search.ProviderSpotify: {catalogAlbum(search.ProviderSpotify, "sp-a", "Fully Loaded")},
	}
	svc := searchtest.NewArtistContent(t, albumsFrom(albums), searchtest.ArtistEvents(store, bg))
	if _, err := svc.GetAlbums(context.Background(), search.ProviderSpotify, "abc123", map[search.ProviderName]string{search.ProviderSpotify: "sp-che"}, 50); err != nil {
		t.Fatalf("GetAlbums: %v", err)
	}
	bg.Wait()
	return logs.String()
}

func TestDiscographyEmit_AFailedAppendIsLoggedAndASuccessfulOneIsNot(t *testing.T) {
	const failed = `"msg":"discography.telemetry.emit_failed"`

	if got := discographyEmitLogs(t, errors.New("store down")); !strings.Contains(got, failed) {
		t.Errorf("logs = %s, want an emit_failed warning", got)
	}
	if got := discographyEmitLogs(t, nil); strings.Contains(got, failed) {
		t.Errorf("logs = %s, want no emit_failed warning", got)
	}
}

type panickingEventStore struct{ asked chan struct{} }

func (s panickingEventStore) Append(context.Context, search.InteractionEvent) error {
	close(s.asked)
	panic("event store exploded")
}

func TestDiscographyEmit_BestEffortContainsPanic(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := panickingEventStore{asked: make(chan struct{})}
	svc := identityFanOut(t, func(search.ProviderName) bool { return false }, searchtest.ArtistEvents(store, bg))

	resp, err := svc.GetAlbums(context.Background(), search.ProviderDeezer, "id-deezer", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v, want nil (emit must never fail the response)", err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) == 0 {
		t.Fatalf("status = %s, items = %d, want ok with the merged discography intact", resp.Status, len(resp.Items))
	}
	select {
	case <-store.asked:
	case <-time.After(5 * time.Second):
		t.Fatal("event store never asked to append")
	}
}

func TestDiscographyEmit_NoEventStoreStillServesAlbums(t *testing.T) {
	svc := identityFanOut(t, func(search.ProviderName) bool { return false })
	if _, err := svc.GetAlbums(context.Background(), search.ProviderDeezer, "id-deezer", nil, 50); err != nil {
		t.Fatalf("GetAlbums error = %v, want nil", err)
	}
}

func assertPayloadInt(t *testing.T, payload map[string]any, key string, want int) {
	t.Helper()
	got, ok := payload[key].(int)
	if !ok {
		t.Fatalf("payload[%q] type = %T, want int", key, payload[key])
	}
	if got != want {
		t.Errorf("payload[%q] = %d, want %d", key, got, want)
	}
}

func TestNewDiscographyTelemetry_RefusesANilClock(t *testing.T) {
	_, err := service.NewDiscographyTelemetry(&searchtest.FakeEventStore{}, searchtest.NewTracker(t), nil)

	if !errors.Is(err, service.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "clock") {
		t.Errorf("without a clock: err = %v, want artistpage.ErrCollaboratorRequired naming the clock", err)
	}
}
