package service

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type recordingPlaylistPublisher struct {
	mu     sync.Mutex
	events []struct {
		typ     string
		payload map[string]any
	}
}

func (p *recordingPlaylistPublisher) Publish(_ shared.UserId, eventType string, payload map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, struct {
		typ     string
		payload map[string]any
	}{eventType, payload})
}

func (p *recordingPlaylistPublisher) last(typ string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.events) - 1; i >= 0; i-- {
		if p.events[i].typ == typ {
			return p.events[i].payload
		}
	}
	return nil
}

// TestPlaylistService_PublishesMutationEvents is the F13 regression: rename,
// remove-track, and reorder emitted no events, so those changes never propagated
// to another device. Each must now publish with the changed fields.
func TestPlaylistService_PublishesMutationEvents(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Run("rename", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		pl, _ := domain.NewPlaylist(userId, "Old")
		plRepo.Seed(pl)
		svc := NewPlaylistLifecycleService(plRepo, WithPlaylistLifecycleEvents(pub))

		if _, _, err := svc.Rename(ctx, userId, pl.ID, "New Name"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		p := pub.last("playlist_renamed")
		if p == nil || p["playlist_id"] != pl.ID.String() || p["name"] != "New Name" {
			t.Fatalf("playlist_renamed payload = %v", p)
		}
	})

	t.Run("remove track", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		track, _ := domain.NewTrack(userId, "T", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL")
		_ = pl.AddTrack(track.ID)
		plRepo.SeedWithTracks(pl, []*domain.Track{track})
		svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo(), WithPlaylistMembershipEvents(pub))

		if err := svc.RemoveTrack(ctx, userId, pl.ID, track.ID); err != nil {
			t.Fatalf("remove track: %v", err)
		}
		p := pub.last("track_removed_from_playlist")
		if p == nil || p["playlist_id"] != pl.ID.String() || p["track_id"] != track.ID.String() {
			t.Fatalf("track_removed_from_playlist payload = %v", p)
		}
	})

	t.Run("reorder", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		t1, _ := domain.NewTrack(userId, "T1", "A", "")
		t2, _ := domain.NewTrack(userId, "T2", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL")
		_ = pl.AddTrack(t1.ID)
		_ = pl.AddTrack(t2.ID)
		plRepo.SeedWithTracks(pl, []*domain.Track{t1, t2})
		svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo(), WithPlaylistMembershipEvents(pub))

		if err := svc.Reorder(ctx, userId, pl.ID, []domain.TrackId{t2.ID, t1.ID}); err != nil {
			t.Fatalf("reorder: %v", err)
		}
		p := pub.last("playlist_reordered")
		if p == nil || p["playlist_id"] != pl.ID.String() {
			t.Fatalf("playlist_reordered payload = %v", p)
		}
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != t2.ID.String() || ids[1] != t1.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], t2.ID.String(), t1.ID.String())
		}
	})
}
