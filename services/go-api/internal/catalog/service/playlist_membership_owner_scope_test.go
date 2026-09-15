package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ownerBlindReadRepo simulates a regressed service-layer ownership check: its
// Exists and GetTrackOrder answer for any playlist regardless of owner, so the
// service's reads no longer stop a foreign caller. The embedded fake's writes
// stay owner-scoped, standing in for the owner-scoped SQL underneath.
type ownerBlindReadRepo struct {
	*catalogtest.PlaylistRepo
}

func (r ownerBlindReadRepo) Exists(_ context.Context, id domain.PlaylistId, _ shared.UserId) (bool, error) {
	_, ok := r.Playlists[id.String()]
	return ok, nil
}

func (r ownerBlindReadRepo) GetTrackOrder(_ context.Context, id domain.PlaylistId, _ shared.UserId) ([]domain.TrackId, bool, error) {
	p, ok := r.Playlists[id.String()]
	if !ok {
		return nil, false, nil
	}
	ids := make([]domain.TrackId, len(p.Tracks))
	for i, t := range p.Tracks {
		ids[i] = t.TrackId
	}
	return ids, true, nil
}

type recordingPublisher struct{ types []string }

func (p *recordingPublisher) Publish(_ shared.UserId, eventType string, _ map[string]any) {
	p.types = append(p.types, eventType)
}

// TestPlaylistMembershipService_ForeignWriteRefusedBelowLoadCheck proves the
// defense in depth from issue #1044: even when the read-side ownership check is
// bypassed, every membership write passes the caller's userId down, the
// owner-scoped repository refuses it, and the service answers
// ErrPlaylistNotFound without persisting anything or publishing an event.
func TestPlaylistMembershipService_ForeignWriteRefusedBelowLoadCheck(t *testing.T) {
	ctx := context.Background()
	victim := testUserId()
	attacker := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

	cases := []struct {
		name string
		call func(*PlaylistMembershipService, domain.PlaylistId, []domain.TrackId, domain.TrackId) error
	}{
		{"AddTrack", func(s *PlaylistMembershipService, pl domain.PlaylistId, _ []domain.TrackId, own domain.TrackId) error {
			return s.AddTrack(ctx, attacker, pl, own)
		}},
		{"AddTracks", func(s *PlaylistMembershipService, pl domain.PlaylistId, _ []domain.TrackId, own domain.TrackId) error {
			_, err := s.AddTracks(ctx, attacker, pl, []domain.TrackId{own})
			return err
		}},
		{"RemoveTrack", func(s *PlaylistMembershipService, pl domain.PlaylistId, members []domain.TrackId, _ domain.TrackId) error {
			return s.RemoveTrack(ctx, attacker, pl, members[0])
		}},
		{"RemoveTracks", func(s *PlaylistMembershipService, pl domain.PlaylistId, members []domain.TrackId, _ domain.TrackId) error {
			_, err := s.RemoveTracks(ctx, attacker, pl, members)
			return err
		}},
		{"Reorder", func(s *PlaylistMembershipService, pl domain.PlaylistId, members []domain.TrackId, _ domain.TrackId) error {
			return s.Reorder(ctx, attacker, pl, []domain.TrackId{members[1], members[0]})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			pl := seedPlaylist(t, plRepo, victim, "Victim's Playlist")
			members := []domain.TrackId{domain.NewTrackId(), domain.NewTrackId()}
			for _, id := range members {
				if err := pl.AddTrack(id, time.Now()); err != nil {
					t.Fatalf("seed AddTrack: %v", err)
				}
			}
			own := seedTrack(t, trRepo, attacker, "Attacker Track", "Artist", "Album")
			pub := &recordingPublisher{}
			svc := NewPlaylistMembershipService(ownerBlindReadRepo{plRepo}, trRepo, WithPlaylistMembershipEvents(pub))

			err := tc.call(svc, pl.ID, members, own.ID)

			if !errors.Is(err, ErrPlaylistNotFound) {
				t.Fatalf("%s by non-owner: err = %v, want ErrPlaylistNotFound", tc.name, err)
			}
			if len(plRepo.Added) != 0 || len(plRepo.Removed) != 0 {
				t.Fatalf("%s by non-owner persisted: added=%v removed=%v", tc.name, plRepo.Added, plRepo.Removed)
			}
			if len(pub.types) != 0 {
				t.Fatalf("%s by non-owner published events: %v", tc.name, pub.types)
			}
		})
	}
}
