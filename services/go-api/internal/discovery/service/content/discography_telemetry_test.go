package content_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"testing"
	"time"
)

func catalogAlbum(provider discovery.ProviderName, id, title string) discovery.SearchResult {
	return discovery.SearchResult{
		Kind:     discovery.ResultKindAlbum,
		Title:    title,
		Subtitle: "Che",
		Sources:  []discovery.SourceRef{{Provider: provider, ExternalID: id}},
		Extras:   map[string]any{},
	}
}

type recordingEventStore struct {
	events chan discovery.InteractionEvent
}

func newRecordingEventStore() *recordingEventStore {
	return &recordingEventStore{events: make(chan discovery.InteractionEvent, 8)}
}

func (s *recordingEventStore) Append(_ context.Context, e discovery.InteractionEvent) error {
	s.events <- e
	return nil
}

func (s *recordingEventStore) next(t *testing.T) discovery.InteractionEvent {
	t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no event recorded within 5s")
		return discovery.InteractionEvent{}
	}
}

func albumsFrom(albums map[discovery.ProviderName][]discovery.SearchResult) map[discovery.ProviderName]discovery.ArtistContentProvider {
	providers := make(map[discovery.ProviderName]discovery.ArtistContentProvider, len(albums))
	for name, served := range albums {
		providers[name] = artistContentFuncs{
			albums: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
				return served, nil
			},
		}
	}
	return providers
}

func observeDiscography(t *testing.T, albums map[discovery.ProviderName][]discovery.SearchResult) discovery.InteractionEvent {
	t.Helper()
	known := map[discovery.ProviderName]string{}
	for name := range albums {
		known[name] = name.String() + "-che"
	}
	store := newRecordingEventStore()
	svc := discovery.NewGetArtistContentService(albumsFrom(albums), discovery.WithContentEventStore(store))
	if _, err := svc.GetAlbums(context.Background(), discovery.ProviderSpotify, "abc123", known, 50); err != nil {
		t.Fatalf("GetAlbums: %v", err)
	}
	return store.next(t)
}

func TestDiscographyEmit_PayloadShapeAndNoUserID(t *testing.T) {
	ev := observeDiscography(t, map[discovery.ProviderName][]discovery.SearchResult{
		discovery.ProviderSpotify: {
			catalogAlbum(discovery.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(discovery.ProviderSpotify, "sp-b", "Sundown"),
		},
		discovery.ProviderMusicBrainz: {
			catalogAlbum(discovery.ProviderMusicBrainz, "mb-a", "Fully Loaded"),
		},
	})

	if ev.Type != discovery.EventTypeDiscographyObserved {
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
	ev := observeDiscography(t, map[discovery.ProviderName][]discovery.SearchResult{
		discovery.ProviderSpotify: {
			catalogAlbum(discovery.ProviderSpotify, "sp-a", "Fully Loaded"),
			catalogAlbum(discovery.ProviderSpotify, "sp-c", "Sundown"),
		},
		discovery.ProviderDeezer: {
			catalogAlbum(discovery.ProviderDeezer, "dz-b", "Closer to God"),
		},
		discovery.ProviderMusicBrainz: {
			catalogAlbum(discovery.ProviderMusicBrainz, "mb-c", "Sundown"),
		},
	})

	assertPayloadInt(t, ev.Payload, "releases", 3)
	assertPayloadInt(t, ev.Payload, "single_provider", 2)
	assertPayloadInt(t, ev.Payload, "single_provider_no_id", 0)
}

type panickingEventStore struct{ asked chan struct{} }

func (s panickingEventStore) Append(context.Context, discovery.InteractionEvent) error {
	close(s.asked)
	panic("event store exploded")
}

func TestDiscographyEmit_BestEffortContainsPanic(t *testing.T) {
	store := panickingEventStore{asked: make(chan struct{})}
	svc := identityFanOut(func(discovery.ProviderName) bool { return false }, discovery.WithContentEventStore(store))

	resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v, want nil (emit must never fail the response)", err)
	}
	if resp.Status != discovery.ProviderStatusOK || len(resp.Items) == 0 {
		t.Fatalf("status = %s, items = %d, want ok with the merged discography intact", resp.Status, len(resp.Items))
	}
	select {
	case <-store.asked:
	case <-time.After(5 * time.Second):
		t.Fatal("event store never asked to append")
	}
}

func TestDiscographyEmit_NoEventStoreStillServesAlbums(t *testing.T) {
	svc := identityFanOut(func(discovery.ProviderName) bool { return false })
	if _, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "id-deezer", nil, 50); err != nil {
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
