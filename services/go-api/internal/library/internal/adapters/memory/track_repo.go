package memory

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"sort"
	"strings"
)

type TrackRepo struct {
	Tracks map[string]*domain.Track

	claimable  map[string]bool
	libraryCap int

	ErrOnAdd    error
	ErrOnCount  error
	ErrOnGetBy  error
	ErrOnList   error
	ErrOnUpdate error
	ErrOnDelete error

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
	_ ports.LibraryViewRepository    = (*TrackRepo)(nil)
	_ ports.FeaturedArtistRepository = (*TrackRepo)(nil)
)

func NewTrackRepo(opts ...func(*TrackRepo)) *TrackRepo {
	r := &TrackRepo{
		Tracks:     make(map[string]*domain.Track),
		claimable:  make(map[string]bool),
		libraryCap: domain.MaxTracksPerUser,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithLibraryCap(libraryCap int) func(*TrackRepo) {
	return func(r *TrackRepo) {
		if libraryCap < 1 {
			panic("memory: WithLibraryCap requires a positive cap")
		}
		r.libraryCap = libraryCap
	}
}

func (r *TrackRepo) Add(_ context.Context, track *domain.Track) (*domain.Track, bool, error) {
	if r.ErrOnAdd != nil {
		return nil, false, r.ErrOnAdd
	}
	held := 0
	for _, t := range r.Tracks {
		if t.UserId() != track.UserId() {
			continue
		}
		if sameIdempotencyKey(t, track) || t.DedupKey() == track.DedupKey() {
			return t, false, nil
		}
		held++
	}
	if held >= r.libraryCap {
		return nil, false, domain.ErrLibraryFull
	}
	r.Tracks[track.ID().String()] = track
	r.claimable[track.ID().String()] = track.DownloadStatus() == domain.AcquisitionPending
	return track, true, nil
}

func (r *TrackRepo) Claimable(id domain.TrackId) bool {
	return r.claimable[id.String()]
}

func sameIdempotencyKey(a, b *domain.Track) bool {
	return a.IdempotencyKey() != nil && b.IdempotencyKey() != nil &&
		*a.IdempotencyKey() == *b.IdempotencyKey()
}

func (r *TrackRepo) GetByID(_ context.Context, id domain.TrackId, userId sharedids.UserId) (*domain.Track, error) {
	if r.ErrOnGetBy != nil {
		return nil, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.ID() == id && t.UserId() == userId {
			return t, nil
		}
	}
	return nil, domain.ErrTrackNotFound
}

func (r *TrackRepo) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	if r.ErrOnGetBy != nil {
		return false, r.ErrOnGetBy
	}
	for _, t := range r.Tracks {
		if t.ID() != excludeTrackID && t.AudioRef() != nil && *t.AudioRef() == audioRef {
			return true, nil
		}
	}
	return false, nil
}

func (r *TrackRepo) CountForUser(_ context.Context, userId sharedids.UserId, atMost int) (int, error) {
	if r.ErrOnCount != nil {
		return 0, r.ErrOnCount
	}
	held := 0
	for _, t := range r.Tracks {
		if t.UserId() != userId {
			continue
		}
		held++
		if held == atMost {
			break
		}
	}
	return held, nil
}

func (r *TrackRepo) ListForUser(_ context.Context, userId sharedids.UserId, limit, offset int) ([]*domain.Track, int, error) {
	if r.ErrOnList != nil {
		return nil, 0, r.ErrOnList
	}
	all := r.newestFirst(userId)
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

func (r *TrackRepo) newestFirst(userId sharedids.UserId) []*domain.Track {
	var all []*domain.Track
	for _, t := range r.Tracks {
		if t.UserId() == userId {
			all = append(all, t)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].AddedAt().Equal(all[j].AddedAt()) {
			return all[i].AddedAt().After(all[j].AddedAt())
		}
		return all[i].ID().String() > all[j].ID().String()
	})
	return all
}

func (r *TrackRepo) ListFilteredForUser(ctx context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]*domain.Track, int, error) {
	tracks, total, err := r.ListForUser(ctx, userId, query.Limit, query.Offset)
	if err != nil || query.Search == "" {
		return tracks, total, err
	}
	needle := strings.ToLower(query.Search)
	var matched []*domain.Track
	for _, t := range tracks {
		if strings.Contains(strings.ToLower(t.Title()), needle) ||
			strings.Contains(strings.ToLower(t.Artist()), needle) ||
			strings.Contains(strings.ToLower(t.Album()), needle) {
			matched = append(matched, t)
		}
	}
	return matched, len(matched), nil
}

func (r *TrackRepo) ListAlbumsForUser(_ context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error) {
	r.LastAlbumsQuery = query
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.AlbumGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId() != userId || t.Album() == "" {
			continue
		}
		artist := albumArtist(t)
		key := strings.ToLower(t.Album()) + "|||" + strings.ToLower(artist)
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = newAlbumGroup(key, artist, t)
		order = append(order, key)
	}
	return inOrder(byKey, order), nil
}

func newAlbumGroup(key, artist string, t *domain.Track) *domain.AlbumGroup {
	return &domain.AlbumGroup{
		Key: key, Album: t.Album(), Artist: artist,
		ArtworkURL: t.ArtworkURL(), Year: t.Year(),
		TrackCount: 1, MostRecentAddedAt: t.AddedAt(),
	}
}

func albumArtist(t *domain.Track) string {
	if t.AlbumArtist() != nil {
		return *t.AlbumArtist()
	}
	return t.Artist()
}

func (r *TrackRepo) ListArtistsForUser(_ context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error) {
	r.LastArtistsQuery = query
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	byKey := map[string]*domain.ArtistGroup{}
	var order []string
	for _, t := range r.Tracks {
		if t.UserId() != userId {
			continue
		}
		key := strings.ToLower(t.Artist())
		if g, ok := byKey[key]; ok {
			g.TrackCount++
			continue
		}
		byKey[key] = newArtistGroup(key, t)
		order = append(order, key)
	}
	return inOrder(byKey, order), nil
}

func newArtistGroup(key string, t *domain.Track) *domain.ArtistGroup {
	return &domain.ArtistGroup{
		Key: key, Artist: t.Artist(), ArtworkURL: t.ArtworkURL(),
		TrackCount: 1, MostRecentAddedAt: t.AddedAt(),
	}
}

func inOrder[G any](byKey map[string]*G, order []string) []G {
	out := make([]G, 0, len(order))
	for _, key := range order {
		if group := byKey[key]; group != nil {
			out = append(out, *group)
		}
	}
	return out
}

func (r *TrackRepo) ListByIDs(_ context.Context, userId sharedids.UserId, ids []domain.TrackId) ([]*domain.Track, error) {
	if r.ErrOnList != nil {
		return nil, r.ErrOnList
	}
	var out []*domain.Track
	for _, id := range ids {
		if t, ok := r.Tracks[id.String()]; ok && t.UserId() == userId {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *TrackRepo) Update(_ context.Context, track *domain.Track, expectedVersion int) error {
	if r.ErrOnUpdate != nil {
		return r.ErrOnUpdate
	}
	existing, ok := r.Tracks[track.ID().String()]
	if !ok || existing.UserId() != track.UserId() {
		return fmt.Errorf("track %s not found or was deleted", track.ID().String())
	}
	if existing.Version() != expectedVersion {
		return fmt.Errorf("%w: track %s expected version %d, found %d",
			ports.ErrTrackVersionConflict, track.ID().String(), expectedVersion, existing.Version())
	}
	track.RecordVersion(expectedVersion + 1)
	r.Tracks[track.ID().String()] = track
	return nil
}

func rewrite(t *domain.Track, change func(*domain.TrackSnapshot)) {
	snapshot := t.Snapshot()
	change(&snapshot)
	*t = *domain.RestoreTrack(snapshot)
}

func (r *TrackRepo) Requeue(_ context.Context, id domain.TrackId) error {
	for _, t := range r.Tracks {
		if t.ID() != id {
			continue
		}
		failed := t.DownloadStatus() == domain.AcquisitionFailed
		if !failed && (t.FailureReason() == nil || *t.FailureReason() == "") {
			continue
		}
		rewrite(t, func(s *domain.TrackSnapshot) {
			if failed {
				s.DownloadStatus = domain.AcquisitionPending
			}
			s.FailureReason = nil
			s.Version++
		})
	}
	return nil
}

func (r *TrackRepo) UpdateFenced(ctx context.Context, track *domain.Track, expectedVersion, _ int) error {
	return r.Update(ctx, track, expectedVersion)
}

func (r *TrackRepo) SetTrackNumber(_ context.Context, id domain.TrackId, userId sharedids.UserId, n int) (bool, error) {
	t, ok := r.Tracks[id.String()]
	if !ok || t.UserId() != userId || t.TrackNumber() != nil {
		return false, nil
	}
	rewrite(t, func(s *domain.TrackSnapshot) { s.TrackNumber = &n })
	return true, nil
}

func (r *TrackRepo) Delete(_ context.Context, id domain.TrackId, userId sharedids.UserId) (bool, *string, error) {
	if r.ErrOnDelete != nil {
		return false, nil, r.ErrOnDelete
	}
	key := id.String()
	t, ok := r.Tracks[key]
	if !ok || t.UserId() != userId {
		return false, nil, nil
	}
	audioRef := t.AudioRef()
	delete(r.Tracks, key)
	return true, audioRef, nil
}

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

func (r *TrackRepo) Seed(track *domain.Track) {
	r.Tracks[track.ID().String()] = track
}
