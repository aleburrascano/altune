package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"strings"
	"time"
)

type TrackRepo struct {
	Tracks map[string]*domain.Track

	ErrOnAdd       error
	ErrOnCount     error
	ErrOnGetBy     error
	ErrOnList      error
	ErrOnUpdate    error
	ErrOnDelete    error
	ErrOnFailStale error

	// EnforceVersionCAS opts this fake into the optimistic-lock predicate so a
	// unit test can drive the ErrTrackVersionConflict path in memory. Off by
	// default: most unit tests do not model versions and rely on last-writer-wins.
	EnforceVersionCAS bool

	LastAlbumsQuery  domain.LibraryQuery
	LastArtistsQuery domain.LibraryQuery
}

var (
	_ ports.TrackAdder               = (*TrackRepo)(nil)
	_ ports.TrackCounter             = (*TrackRepo)(nil)
	_ ports.TrackAddUpdater          = (*TrackRepo)(nil)
	_ ports.TrackGetter              = (*TrackRepo)(nil)
	_ ports.TrackBatchGetter         = (*TrackRepo)(nil)
	_ ports.TrackLister              = (*TrackRepo)(nil)
	_ ports.TrackUpdater             = (*TrackRepo)(nil)
	_ ports.TrackNumberSetter        = (*TrackRepo)(nil)
	_ ports.TrackDeleter             = (*TrackRepo)(nil)
	_ ports.TrackAudioDeleter        = (*TrackRepo)(nil)
	_ ports.TrackReadWriter          = (*TrackRepo)(nil)
	_ ports.TrackLookup              = (*TrackRepo)(nil)
	_ ports.LibraryLensRepository    = (*TrackRepo)(nil)
	_ ports.FeaturedArtistRepository = (*TrackRepo)(nil)
	_ ports.StalePendingFailer       = (*TrackRepo)(nil)
)

func NewTrackRepo() *TrackRepo {
	return &TrackRepo{Tracks: make(map[string]*domain.Track)}
}

func (r *TrackRepo) Add(_ context.Context, track *domain.Track) (*domain.Track, bool, error) {
	if r.ErrOnAdd != nil {
		return nil, false, r.ErrOnAdd
	}
	for _, t := range r.Tracks {
		if t.UserId != track.UserId {
			continue
		}
		if sameIdempotencyKey(t, track) || t.DedupKey == track.DedupKey {
			return t, false, nil
		}
	}
	r.Tracks[track.ID.String()] = track
	return track, true, nil
}

func sameIdempotencyKey(a, b *domain.Track) bool {
	return a.IdempotencyKey != nil && b.IdempotencyKey != nil &&
		*a.IdempotencyKey == *b.IdempotencyKey
}

func (r *TrackRepo) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	if r.ErrOnGetBy != nil {
		return nil, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.ID == id && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

// AudioRefInUse mirrors the adapter's cross-owner reference check: any track
// but excludeTrackID pointing at the key holds it.
func (r *TrackRepo) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	if r.ErrOnGetBy != nil {
		return false, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.ID != excludeTrackID && t.AudioRef != nil && *t.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

// CountForUser mirrors the adapter's bounded count: it stops at atMost, so a
// caller cannot tell a library exactly at the cap from one far past it.
func (r *TrackRepo) CountForUser(_ context.Context, userId shared.UserId, atMost int) (int, error) {
	if r.ErrOnCount != nil {
		return 0, r.ErrOnCount
	}
	held := 0
	for _, t := range r.Tracks {
		if t.UserId != userId {
			continue
		}
		held++
		if held == atMost {
			break
		}
	}
	return held, nil
}

func (r *TrackRepo) ListForUser(_ context.Context, userId shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	if r.ErrOnList != nil {
		return nil, 0, r.ErrOnList
	}
	var all []*domain.Track
	for _, t := range r.Tracks {
		if t.UserId == userId {
			all = append(all, t)
		}
	}
	total := len(all)
	if offset >= len(all) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (r *TrackRepo) ListFilteredForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]*domain.Track, int, error) {
	tracks, total, err := r.ListForUser(ctx, userId, query.Limit, query.Offset)
	if err != nil || query.Search == "" {
		return tracks, total, err
	}
	needle := strings.ToLower(query.Search)
	var matched []*domain.Track
	for _, t := range tracks {
		if strings.Contains(strings.ToLower(t.Title), needle) ||
			strings.Contains(strings.ToLower(t.Artist), needle) ||
			strings.Contains(strings.ToLower(t.Album), needle) {
			matched = append(matched, t)
		}
	}
	return matched, len(matched), nil
}

func (r *TrackRepo) ListAlbumsForUser(_ context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error) {
	r.LastAlbumsQuery = query
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.AlbumGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId != userId || t.Album == "" {
			continue
		}
		artist := t.Artist
		if t.AlbumArtist != nil {
			artist = *t.AlbumArtist
		}
		key := strings.ToLower(t.Album) + "|||" + strings.ToLower(artist)
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = &domain.AlbumGroup{
			Key: key, Album: t.Album, Artist: artist,
			ArtworkURL: t.ArtworkURL, Year: t.Year,
			TrackCount: 1, MostRecentAddedAt: t.AddedAt,
		}
		order = append(order, key)
	}
	out := make([]domain.AlbumGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}

func (r *TrackRepo) ListArtistsForUser(_ context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error) {
	r.LastArtistsQuery = query
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.ArtistGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId != userId {
			continue
		}
		key := strings.ToLower(t.Artist)
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = &domain.ArtistGroup{
			Key: key, Artist: t.Artist, ArtworkURL: t.ArtworkURL,
			TrackCount: 1, MostRecentAddedAt: t.AddedAt,
		}
		order = append(order, key)
	}
	out := make([]domain.ArtistGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}

func (r *TrackRepo) ListByIDs(_ context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	var out []*domain.Track
	for _, id := range ids {
		if t, ok := r.Tracks[id.String()]; ok && t.UserId == userId {
			out = append(out, t)
		}
	}
	return out, nil
}

// Update matches ports.TrackUpdater's optimistic-lock CAS signature. When
// EnforceVersionCAS is set it honours the predicate — a stored version past
// expectedVersion returns ports.ErrTrackVersionConflict — so a test can exercise
// the conflict path in memory; otherwise it stays last-writer-wins for the many
// unit tests that do not model versions. Either way it advances the stored
// version on a successful write, mirroring the real adapter.
func (r *TrackRepo) Update(_ context.Context, track *domain.Track, expectedVersion int) error {
	if r.ErrOnUpdate != nil {
		return r.ErrOnUpdate
	}
	if r.EnforceVersionCAS {
		if existing, ok := r.Tracks[track.ID.String()]; ok && existing.Version != expectedVersion {
			return fmt.Errorf("%w: track %s expected version %d, found %d",
				ports.ErrTrackVersionConflict, track.ID.String(), expectedVersion, existing.Version)
		}
	}
	track.Version = expectedVersion + 1
	r.Tracks[track.ID.String()] = track
	return nil
}

func (r *TrackRepo) SetTrackNumber(_ context.Context, id domain.TrackId, userId shared.UserId, n int) (bool, error) {
	t, ok := r.Tracks[id.String()]
	if !ok || t.UserId != userId || t.TrackNumber != nil {
		return false, nil
	}
	t.TrackNumber = &n
	return true, nil
}

func (r *TrackRepo) Delete(_ context.Context, id domain.TrackId, userId shared.UserId) (bool, *string, error) {
	if r.ErrOnDelete != nil {
		return false, nil, r.ErrOnDelete
	}
	key := id.String()
	t, ok := r.Tracks[key]
	if !ok || t.UserId != userId {
		return false, nil, nil
	}
	audioRef := t.AudioRef
	delete(r.Tracks, key)
	return true, audioRef, nil
}

func (r *TrackRepo) GetByDedupKey(_ context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error) {
	if r.ErrOnGetBy != nil {
		return nil, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.DedupKey == dedupKey && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *TrackRepo) GetByIdempotencyKey(_ context.Context, userId shared.UserId, idempotencyKey string) (*domain.Track, error) {
	if r.ErrOnGetBy != nil {
		return nil, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.UserId == userId && t.IdempotencyKey != nil && *t.IdempotencyKey == idempotencyKey {
			return t, nil
		}
	}
	return nil, nil
}

func (r *TrackRepo) ReplaceFeaturedArtists(_ context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error {
	t, ok := r.Tracks[id.String()]
	if !ok || t.UserId != userId {
		return nil
	}
	t.FeaturedArtists = feats
	return nil
}

func (r *TrackRepo) ListTracksFeaturing(_ context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error) {
	var out []*domain.Track
	for _, t := range r.Tracks {
		if t.UserId != userId {
			continue
		}
		for _, f := range t.FeaturedArtists {
			if f.IdentityKey() == fa.IdentityKey() {
				out = append(out, t)
				break
			}
		}
	}
	return out, nil
}

func (r *TrackRepo) FailStalePending(_ context.Context, cutoff time.Time, reason string) (int, error) {
	if r.ErrOnFailStale != nil {
		return 0, r.ErrOnFailStale
	}
	n := 0
	for _, t := range r.Tracks {
		if t.AcquisitionStatus != domain.AcquisitionPending {
			continue
		}
		if t.AcquisitionStartedAt == nil || !t.AcquisitionStartedAt.Before(cutoff) {
			continue
		}
		if err := t.MarkFailed(reason); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (r *TrackRepo) Seed(track *domain.Track) {
	r.Tracks[track.ID.String()] = track
}
