package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"slices"
	"sync"
	"time"
)

type GeniusSongCatalog struct {
	ArtistID    int
	ArtistFound bool
	Songs       []domain.UnreleasedSong
	MediaByID   map[int]bool
	DetailByID  map[int]domain.GeniusSongDetail
	Err         error
	MediaErr    error
	Gate        chan struct{}

	SearchedFor []string

	mu      sync.Mutex
	lookups []int
}

var _ ports.GeniusSongCatalog = (*GeniusSongCatalog)(nil)

func (g *GeniusSongCatalog) FindArtistID(ctx context.Context, artistName string, _ []string) (int, bool, error) {
	g.mu.Lock()
	g.SearchedFor = append(g.SearchedFor, artistName)
	g.mu.Unlock()
	if g.Gate != nil {
		select {
		case <-g.Gate:
		case <-ctx.Done():
			return 0, false, ctx.Err()
		}
	}
	return g.ArtistID, g.ArtistFound, g.Err
}

func (g *GeniusSongCatalog) ArtistSongs(_ context.Context, _, _ int) ([]domain.UnreleasedSong, error) {
	return append([]domain.UnreleasedSong(nil), g.Songs...), g.Err
}

func (g *GeniusSongCatalog) SongDetail(_ context.Context, songID int) (domain.GeniusSongDetail, error) {
	g.mu.Lock()
	g.lookups = append(g.lookups, songID)
	g.mu.Unlock()
	detail := g.DetailByID[songID]
	detail.HasMedia = detail.HasMedia || g.MediaByID[songID]
	return detail, g.MediaErr
}

func (g *GeniusSongCatalog) DetailLookups() []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Sorted(slices.Values(g.lookups))
}

type ArtistCatalogTitles struct {
	Titles []string
	Err    error
}

var _ ports.ArtistCatalogTitles = (*ArtistCatalogTitles)(nil)

func (c *ArtistCatalogTitles) CatalogTitles(context.Context, domain.ProviderName, string, map[domain.ProviderName]string) ([]string, error) {
	return c.Titles, c.Err
}

type TrackerSongs struct {
	Songs []domain.UnreleasedSong
	Err   error
}

var _ ports.TrackerSongs = (*TrackerSongs)(nil)

func (t *TrackerSongs) ArtistSongs(context.Context, string) ([]domain.UnreleasedSong, error) {
	return append([]domain.UnreleasedSong(nil), t.Songs...), t.Err
}

type StoreWrite struct {
	Key       ports.UnreleasedArtistKey
	Songs     []domain.UnreleasedSong
	Complete  bool
	StartedAt time.Time
}

type StoredArtist struct {
	Songs    []domain.UnreleasedSong
	Complete bool
	Built    bool
}

// UnreleasedStore is an in-memory ports.UnreleasedStore that records every call in order.
type UnreleasedStore struct {
	Artists   map[ports.UnreleasedArtistKey]StoredArtist
	RecordErr error
	ReadErr   error
	WriteErr  error

	mu     sync.Mutex
	calls  []string
	writes []StoreWrite
}

var _ ports.UnreleasedStore = (*UnreleasedStore)(nil)

func (u *UnreleasedStore) Record(context.Context, ports.UnreleasedArtistKey, string, map[domain.ProviderName]string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, "record")
	return u.RecordErr
}

func (u *UnreleasedStore) Read(_ context.Context, key ports.UnreleasedArtistKey) ([]domain.UnreleasedSong, bool, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, "read")
	artist := u.Artists[key]
	return slices.Clone(artist.Songs), artist.Complete, artist.Built, u.ReadErr
}

func (u *UnreleasedStore) Write(_ context.Context, key ports.UnreleasedArtistKey, songs []domain.UnreleasedSong, complete bool, startedAt time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, "write")
	if u.WriteErr != nil {
		return u.WriteErr
	}
	u.writes = append(u.writes, StoreWrite{Key: key, Songs: slices.Clone(songs), Complete: complete, StartedAt: startedAt})
	if u.Artists == nil {
		u.Artists = map[ports.UnreleasedArtistKey]StoredArtist{}
	}
	u.Artists[key] = StoredArtist{Songs: slices.Clone(songs), Complete: complete, Built: true}
	return nil
}

func (u *UnreleasedStore) Due(context.Context, time.Time, int) ([]ports.UnreleasedDueArtist, error) {
	return nil, nil
}

func (u *UnreleasedStore) Calls() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.calls)
}

func (u *UnreleasedStore) Writes() []StoreWrite {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.writes)
}
