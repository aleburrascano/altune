package memory

import (
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
)

func (r *TrackRepo) ReplaceFeaturedArtists(_ context.Context, id domain.TrackId, userId sharedids.UserId, feats []domain.FeaturedArtist) error {
	t, ok := r.Tracks[id.String()]
	if !ok || t.UserId() != userId {
		return nil
	}
	rewrite(t, func(s *domain.TrackSnapshot) { s.FeaturedArtists = feats })
	return nil
}

func (r *TrackRepo) ListTracksFeaturing(_ context.Context, userId sharedids.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error) {
	var out []*domain.Track
	for _, t := range r.Tracks {
		if t.UserId() != userId {
			continue
		}
		for _, f := range t.FeaturedArtists() {
			if f.IdentityKey() == fa.IdentityKey() {
				out = append(out, t)
				break
			}
		}
	}
	return out, nil
}
