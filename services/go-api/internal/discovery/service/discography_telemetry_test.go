package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"sync"
	"testing"
)

// recordingEventStore captures appended events for assertion. Append is called
// from the emit's detached goroutine, so access is mutex-guarded.
type recordingEventStore struct {
	mu     sync.Mutex
	events []domain.InteractionEvent
}

func (s *recordingEventStore) Append(_ context.Context, e domain.InteractionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *recordingEventStore) only(t *testing.T) domain.InteractionEvent {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) != 1 {
		t.Fatalf("recorded %d events, want exactly 1", len(s.events))
	}
	return s.events[0]
}

// The best-effort test reuses panickingEventStore (search_orchestrator_more_test.go),
// a store whose Append blows up, to prove the emit contains a panic and never
// reaches the response path.

func mergedRelease(providers ...domain.ProviderName) MergedRelease {
	set := make(map[domain.ProviderName]bool, len(providers))
	for _, p := range providers {
		set[p] = true
	}
	return MergedRelease{Providers: set}
}

// TestDiscographyEmit_PayloadShapeAndNoUserID plants the no-user-id-in-payload
// core rule and asserts the pinned discography_observed shape: the structural
// signal carries the artist ref, release/suspect counts, per-provider counts and
// a timestamp — and no user identity anywhere in the payload. The event row uses
// the synthetic system identity, never a real account.
func TestDiscographyEmit_PayloadShapeAndNoUserID(t *testing.T) {
	store := &recordingEventStore{}
	tel := newDiscographyTelemetry(store)

	merged := []MergedRelease{
		mergedRelease(domain.ProviderSpotify, domain.ProviderMusicBrainz),
		mergedRelease(domain.ProviderSpotify), // contamination suspect: one provider
	}
	tel.emit(context.Background(), "spotify:abc123", merged)
	tel.bg.wait()

	ev := store.only(t)
	if ev.Type != domain.EventTypeDiscographyObserved {
		t.Fatalf("event type = %v, want discography_observed", ev.Type)
	}
	if !ev.UserId.IsSystem() {
		t.Errorf("event user id = %s, want the synthetic system identity", ev.UserId)
	}

	assertPayloadInt(t, ev.Payload, "releases", 2)
	assertPayloadInt(t, ev.Payload, "single_provider", 1)
	if ref, _ := ev.Payload["artist_ref"].(string); ref != "spotify:abc123" {
		t.Errorf("artist_ref = %q, want spotify:abc123", ref)
	}
	if _, ok := ev.Payload["last_seen"]; !ok {
		t.Error("payload missing last_seen")
	}
	counts, ok := ev.Payload["provider_counts"].(map[string]int)
	if !ok {
		t.Fatalf("provider_counts type = %T, want map[string]int", ev.Payload["provider_counts"])
	}
	if counts["spotify"] != 2 || counts["musicbrainz"] != 1 {
		t.Errorf("provider_counts = %v, want spotify:2 musicbrainz:1", counts)
	}

	// The structural signal must carry no user identity, present or hashed.
	for _, k := range []string{"user_id", "userId", "user", "uid", "user_hash", "hashed_user_id"} {
		if _, present := ev.Payload[k]; present {
			t.Errorf("payload leaks user identity via key %q: %v", k, ev.Payload[k])
		}
	}
	if len(ev.Payload) != 5 {
		t.Errorf("payload has %d keys, want exactly the 5 pinned fields: %v", len(ev.Payload), ev.Payload)
	}
}

// TestDiscographyEmit_BestEffortContainsPanic plants the best-effort-emit core
// rule end to end: a store whose Append panics must not fail or crash the artist
// discography response. GetAlbums returns its merged albums normally and the
// contained panic never escapes the detached emit.
func TestDiscographyEmit_BestEffortContainsPanic(t *testing.T) {
	svc := identityFanOut(func(domain.ProviderName) bool { return false }, WithContentEventStore(&panickingEventStore{}))

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v, want nil (emit must never fail the response)", err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) == 0 {
		t.Fatalf("status = %s, items = %d, want ok with the merged discography intact", resp.Status, len(resp.Items))
	}
	// Drain the detached emit: an uncontained panic here would crash the test
	// binary rather than fail this assertion.
	svc.discographyTelemetry.bg.wait()
}

// TestDiscographyEmit_NilTelemetryIsNoOp confirms the signal is purely additive:
// a service wired without an event store simply records nothing.
func TestDiscographyEmit_NilTelemetryIsNoOp(t *testing.T) {
	svc := identityFanOut(func(domain.ProviderName) bool { return false })
	if svc.discographyTelemetry != nil {
		t.Fatal("discographyTelemetry should be nil when no event store is wired")
	}
	if _, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "id-deezer", "Che", 50); err != nil {
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
