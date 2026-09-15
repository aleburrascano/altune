package service

import (
	"context"
	"sync"
	"testing"
	"time"

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

func TestPlaylistService_PublishesMutationEvents(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Run("rename", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		pl, _ := domain.NewPlaylist(userId, "Old", time.Now())
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
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(track.ID, time.Now())
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
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(t1.ID, time.Now())
		_ = pl.AddTrack(t2.ID, time.Now())
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

	// The membership events below drive mobile optimistic rollback, so their
	// payloads must name exactly the tracks the write changed, in request order.
	t.Run("add tracks names only the inserted tracks in request order", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := seedTrack(t, trRepo, userId, "Member", "A", "")
		b := seedTrack(t, trRepo, userId, "B", "A", "")
		a := seedTrack(t, trRepo, userId, "A", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(member.ID, time.Now())
		plRepo.Seed(pl)
		svc := NewPlaylistMembershipService(plRepo, trRepo, WithPlaylistMembershipEvents(pub))

		if _, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{b.ID, member.ID, a.ID, b.ID}); err != nil {
			t.Fatalf("add tracks: %v", err)
		}
		p := pub.last("tracks_added_to_playlist")
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != b.ID.String() || ids[1] != a.ID.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], b.ID.String(), a.ID.String())
		}
	})

	t.Run("remove tracks names only the removed tracks in request order", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		first, second, third := domain.NewTrackId(), domain.NewTrackId(), domain.NewTrackId()
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		for _, id := range []domain.TrackId{first, second, third} {
			_ = pl.AddTrack(id, time.Now())
		}
		plRepo.Seed(pl)
		svc := NewPlaylistMembershipService(plRepo, catalogtest.NewTrackRepo(), WithPlaylistMembershipEvents(pub))

		if _, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{third, domain.NewTrackId(), first, third}); err != nil {
			t.Fatalf("remove tracks: %v", err)
		}
		p := pub.last("tracks_removed_from_playlist")
		ids, ok := p["track_ids"].([]string)
		if !ok || len(ids) != 2 || ids[0] != third.String() || ids[1] != first.String() {
			t.Fatalf("track_ids = %v, want [%s %s]", p["track_ids"], third.String(), first.String())
		}
	})

	t.Run("no-op membership writes publish nothing", func(t *testing.T) {
		pub := &recordingPlaylistPublisher{}
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		member := seedTrack(t, trRepo, userId, "Member", "A", "")
		pl, _ := domain.NewPlaylist(userId, "PL", time.Now())
		_ = pl.AddTrack(member.ID, time.Now())
		plRepo.Seed(pl)
		svc := NewPlaylistMembershipService(plRepo, trRepo, WithPlaylistMembershipEvents(pub))

		_ = svc.AddTrack(ctx, userId, pl.ID, member.ID)
		_, _ = svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{member.ID})
		_ = svc.RemoveTrack(ctx, userId, pl.ID, domain.NewTrackId())
		_, _ = svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{domain.NewTrackId()})

		if len(pub.events) != 0 {
			t.Fatalf("no-op writes published %v", pub.events)
		}
	})
}
