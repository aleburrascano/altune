package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func catalogAlbum(provider domain.ProviderName, id, title string) domain.SearchResult {
	return memory.NewResult(domain.ResultKindAlbum, title,
		memory.WithSubtitle("Che"),
		memory.WithSource(provider, id, ""),
		memory.WithExtras(map[string]any{}))
}

type artistRecordingEventStore struct {
	events chan domain.InteractionEvent
}

func newRecordingEventStore() *artistRecordingEventStore {
	return &artistRecordingEventStore{events: make(chan domain.InteractionEvent, 8)}
}

func (s *artistRecordingEventStore) Append(_ context.Context, e domain.InteractionEvent) error {
	s.events <- e
	return nil
}

func (s *artistRecordingEventStore) next(t *testing.T) domain.InteractionEvent {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event recorded within 5s")
		return domain.InteractionEvent{}
	}
}

func albumsFrom(albums map[domain.ProviderName][]domain.SearchResult) map[domain.ProviderName]ports.ArtistContentProvider {
	providers := make(map[domain.ProviderName]ports.ArtistContentProvider, len(albums))
	for name, served := range albums {
		providers[name] = artistContentFuncs{
			albums: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
				return served, nil
			},
		}
	}
	return providers
}

func observeDiscography(t *testing.T, albums map[domain.ProviderName][]domain.SearchResult) domain.InteractionEvent {
	t.Helper()
	bg := memory.NewTracker(t)
	known := map[domain.ProviderName]string{}
	for name := range albums {
		known[name] = name.String() + "-che"
	}
	store := newRecordingEventStore()
	svc := memory.NewArtistContent(t, albumsFrom(albums), memory.ArtistEvents(store, bg))
	if _, err := svc.GetAlbums(context.Background(), domain.ProviderSpotify, "abc123", known, 50); err != nil {
		t.Fatalf("GetAlbums: %v", err)
	}
	return store.next(t)
}

func TestNoDiscographyObserver_LeavesTheAlbumsAnswerUnchanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) { noDiscographyObserverLeavesTheAlbumsAnswerUnchanged(t) })
}

func noDiscographyObserverLeavesTheAlbumsAnswerUnchanged(t *testing.T) {
	albums := map[domain.ProviderName][]domain.SearchResult{
		domain.ProviderSpotify: {
			catalogAlbum(domain.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(domain.ProviderSpotify, "sp-b", "Sundown"),
		},
		domain.ProviderMusicBrainz: {catalogAlbum(domain.ProviderMusicBrainz, "mb-a", "Fully Loaded")},
	}
	known := map[domain.ProviderName]string{domain.ProviderSpotify: "sp-che", domain.ProviderMusicBrainz: "mb-che"}
	fetch := func(svc *service.GetArtistContentService) []string {
		resp, err := svc.GetAlbums(context.Background(), domain.ProviderSpotify, "abc123", known, 50)
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

	observed := fetch(memory.NewArtistContent(t, albumsFrom(albums), memory.ArtistEvents(store, memory.NewTracker(t))))
	store.next(t)
	silent, err := service.NewGetArtistContentService(albumsFrom(albums), map[domain.ProviderName]ports.AlbumContentProvider{}, service.SeedPathIdentity{}, service.NoAnchoring{}, service.NoDiscographyObserver{}, service.NewCircuitBreaker(clock.Now))
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
	ev := observeDiscography(t, map[domain.ProviderName][]domain.SearchResult{
		domain.ProviderSpotify: {
			catalogAlbum(domain.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(domain.ProviderSpotify, "sp-b", "Sundown"),
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
	ev := observeDiscography(t, map[domain.ProviderName][]domain.SearchResult{
		domain.ProviderSpotify: {
			catalogAlbum(domain.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(domain.ProviderSpotify, "sp-c", "Sundown"),
		},
		domain.ProviderDeezer: {
			catalogAlbum(domain.ProviderDeezer, "dz-b", "Closer to God"),
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
	logs := memory.CaptureLogs(t)
	bg := memory.NewTracker(t)
	store := &memory.FakeEventStore{Err: appendErr}
	albums := map[domain.ProviderName][]domain.SearchResult{
		domain.ProviderSpotify: {catalogAlbum(domain.ProviderSpotify, "sp-a", "Fully Loaded")},
	}
	svc := memory.NewArtistContent(t, albumsFrom(albums), memory.ArtistEvents(store, bg))
	if _, err := svc.GetAlbums(context.Background(), domain.ProviderSpotify, "abc123", map[domain.ProviderName]string{domain.ProviderSpotify: "sp-che"}, 50); err != nil {
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

func (s panickingEventStore) Append(context.Context, domain.InteractionEvent) error {
	close(s.asked)
	panic("event store exploded")
}

func TestDiscographyEmit_BestEffortContainsPanic(t *testing.T) {
	bg := memory.NewTracker(t)
	store := panickingEventStore{asked: make(chan struct{})}
	svc := identityFanOut(t, func(domain.ProviderName) bool { return false }, memory.ArtistEvents(store, bg))

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 50)
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
	svc := identityFanOut(t, func(domain.ProviderName) bool { return false })
	if _, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", nil, 50); err != nil {
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
	_, err := service.NewDiscographyTelemetry(&memory.FakeEventStore{}, memory.NewTracker(t), nil)

	if !errors.Is(err, domain.ErrArtistPageCollaboratorRequired) || !strings.Contains(err.Error(), "clock") {
		t.Errorf("without a clock: err = %v, want artistpage.ErrCollaboratorRequired naming the clock", err)
	}
}
