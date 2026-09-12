package persistence

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func seedLibraryTrack(t *testing.T, repo *PgxCatalogTrackRepository, userId shared.UserId, spec libraryTrackSpec) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, spec.title, spec.artist, spec.album)
	if err != nil {
		t.Fatalf("NewTrack(%q): %v", spec.title, err)
	}
	if spec.year != nil {
		track.Year = spec.year
	}
	if spec.albumArtist != nil {
		track.AlbumArtist = spec.albumArtist
	}
	track.AddedAt = spec.addedAt
	if _, _, err := repo.Add(context.Background(), track); err != nil {
		t.Fatalf("Add(%q): %v", spec.title, err)
	}
	return track
}

type libraryTrackSpec struct {
	title       string
	artist      string
	album       string
	year        *int
	albumArtist *string
	addedAt     time.Time
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }
func titlesOf(ts []*domain.Track) []string {
	out := make([]string, len(ts))
	for i, tr := range ts {
		out[i] = tr.Title
	}
	return out
}

func assertOrder(t *testing.T, sort domain.LibrarySort, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sort=%s len = %d %v, want %d %v", sort, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sort=%s order = %v, want %v", sort, got, want)
		}
	}
}

func TestPgxTrackRepo_ListFilteredForUser_SortOrder(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Alpha", artist: "Artist A", album: "Alpha Album",
		year: intPtr(2000), addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "bravo", artist: "Artist B", album: "Bravo Album",
		year: intPtr(2020), addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Charlie", artist: "Artist C", album: "Charlie Album",
		year: nil, addedAt: base.Add(-1 * time.Hour),
	})

	cases := []struct {
		sort domain.LibrarySort
		want []string
	}{
		{domain.SortAlphabetical, []string{"Alpha", "bravo", "Charlie"}},
		{domain.SortRecent, []string{"Charlie", "bravo", "Alpha"}},
		{domain.SortYear, []string{"bravo", "Alpha", "Charlie"}},
	}
	for _, c := range cases {
		got, total, err := repo.ListFilteredForUser(ctx, userId, domain.LibraryQuery{Sort: c.sort, Limit: 100})
		if err != nil {
			t.Fatalf("ListFilteredForUser(sort=%s): %v", c.sort, err)
		}
		if total != 3 {
			t.Errorf("sort=%s total = %d, want 3", c.sort, total)
		}
		assertOrder(t, c.sort, titlesOf(got), c.want)
	}
}

func TestPgxTrackRepo_ListFilteredForUser_IlikeMatching(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Concert 50% Discount", artist: "Promo", album: "Deals",
		addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Concert 5000 Seats", artist: "Venue", album: "Deals",
		addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Nightfall", artist: "The BEATLES", album: "Revolver",
		addedAt: base.Add(-1 * time.Hour),
	})

	cases := []struct {
		name   string
		search string
		want   []string
	}{
		{"case insensitive artist", "beatles", []string{"Nightfall"}},
		{"matches album column", "revolver", []string{"Nightfall"}},
		{"percent is literal not wildcard", "50%", []string{"Concert 50% Discount"}},
		{"matches title substring", "Concert", []string{"Concert 5000 Seats", "Concert 50% Discount"}},
		{"no match", "nonexistent", []string{}},
	}
	for _, c := range cases {
		got, total, err := repo.ListFilteredForUser(ctx, userId, domain.LibraryQuery{
			Search: c.search, Sort: domain.SortRecent, Limit: 100,
		})
		if err != nil {
			t.Fatalf("%s: ListFilteredForUser: %v", c.name, err)
		}
		if total != len(c.want) {
			t.Errorf("%s: total = %d, want %d", c.name, total, len(c.want))
		}
		gotTitles := titlesOf(got)
		if len(gotTitles) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, gotTitles, c.want)
		}
		for i := range c.want {
			if gotTitles[i] != c.want[i] {
				t.Fatalf("%s: got %v, want %v", c.name, gotTitles, c.want)
			}
		}
	}
}

func albumsOf(gs []domain.AlbumGroup) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Album
	}
	return out
}

func TestPgxTrackRepo_ListAlbumsForUser_SortOrder(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "t1", artist: "Zed", album: "Avenue",
		year: intPtr(1990), addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "t2", artist: "Amy", album: "Zephyr",
		year: intPtr(2010), addedAt: base.Add(-1 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "t3", artist: "Bob", album: "Melody",
		year: nil, addedAt: base.Add(-2 * time.Hour),
	})

	cases := []struct {
		sort domain.LibrarySort
		want []string
	}{
		{domain.SortAlphabetical, []string{"Avenue", "Melody", "Zephyr"}},
		{domain.SortYear, []string{"Zephyr", "Avenue", "Melody"}},
		{domain.SortRecent, []string{"Zephyr", "Melody", "Avenue"}},
	}
	for _, c := range cases {
		got, err := repo.ListAlbumsForUser(ctx, userId, domain.LibraryQuery{Sort: c.sort})
		if err != nil {
			t.Fatalf("ListAlbumsForUser(sort=%s): %v", c.sort, err)
		}
		assertOrder(t, c.sort, albumsOf(got), c.want)
	}
}

func TestPgxTrackRepo_ListAlbumsForUser_RespectsLimit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "l1", artist: "Ann", album: "Album One", addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "l2", artist: "Ben", album: "Album Two", addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "l3", artist: "Cal", album: "Album Three", addedAt: base.Add(-1 * time.Hour),
	})

	got, err := repo.ListAlbumsForUser(ctx, userId, domain.LibraryQuery{Sort: domain.SortRecent, Limit: 1})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(Limit:1): %v", err)
	}
	if len(got) > 1 {
		t.Fatalf("Limit:1 returned %d albums, want <= 1", len(got))
	}
	if len(got) == 1 && got[0].Album != "Album Three" {
		t.Errorf("Limit:1 sort=recent = %q, want Album Three", got[0].Album)
	}

	page2, err := repo.ListAlbumsForUser(ctx, userId, domain.LibraryQuery{Sort: domain.SortRecent, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(Limit:1,Offset:1): %v", err)
	}
	if len(page2) == 1 && page2[0].Album != "Album Two" {
		t.Errorf("Limit:1 Offset:1 sort=recent = %q, want Album Two", page2[0].Album)
	}
}

func TestPgxTrackRepo_ListArtistsForUser_RespectsLimit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "r1", artist: "Ann", album: "a1", addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "r2", artist: "Ben", album: "a2", addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "r3", artist: "Cal", album: "a3", addedAt: base.Add(-1 * time.Hour),
	})

	got, err := repo.ListArtistsForUser(ctx, userId, domain.LibraryQuery{Sort: domain.SortRecent, Limit: 1})
	if err != nil {
		t.Fatalf("ListArtistsForUser(Limit:1): %v", err)
	}
	if len(got) > 1 {
		t.Fatalf("Limit:1 returned %d artists, want <= 1", len(got))
	}
}

func TestPgxTrackRepo_ListAlbumsForUser_IlikeMatching(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "s1", artist: "Solo Artist", album: "Blue Skies",
		addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "s2", artist: "Session Player", album: "Compiled",
		albumArtist: strPtr("Various Composers"), addedAt: base.Add(-1 * time.Hour),
	})

	byArtist, err := repo.ListAlbumsForUser(ctx, userId, domain.LibraryQuery{Search: "solo"})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(artist search): %v", err)
	}
	if len(byArtist) != 1 || byArtist[0].Album != "Blue Skies" {
		t.Fatalf("artist search got %v, want [Blue Skies]", albumsOf(byArtist))
	}

	byAlbumArtist, err := repo.ListAlbumsForUser(ctx, userId, domain.LibraryQuery{Search: "composers"})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(album_artist search): %v", err)
	}
	if len(byAlbumArtist) != 1 || byAlbumArtist[0].Album != "Compiled" {
		t.Fatalf("album_artist search got %v, want [Compiled]", albumsOf(byAlbumArtist))
	}

	byAlbum, err := repo.ListAlbumsForUser(ctx, userId, domain.LibraryQuery{Search: "BLUE"})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(album search): %v", err)
	}
	if len(byAlbum) != 1 || byAlbum[0].Album != "Blue Skies" {
		t.Fatalf("case-insensitive album search got %v, want [Blue Skies]", albumsOf(byAlbum))
	}
}

func artistsOf(gs []domain.ArtistGroup) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Artist
	}
	return out
}

func TestPgxTrackRepo_ListArtistsForUser_SortOrder(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "a1", artist: "Alpha Artist", album: "x1",
		addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "a2", artist: "Zeta Artist", album: "x2",
		addedAt: base.Add(-1 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "a3", artist: "Mid Artist", album: "x3",
		addedAt: base.Add(-2 * time.Hour),
	})

	cases := []struct {
		sort domain.LibrarySort
		want []string
	}{
		{domain.SortAlphabetical, []string{"Alpha Artist", "Mid Artist", "Zeta Artist"}},
		{domain.SortRecent, []string{"Zeta Artist", "Mid Artist", "Alpha Artist"}},
	}
	for _, c := range cases {
		got, err := repo.ListArtistsForUser(ctx, userId, domain.LibraryQuery{Sort: c.sort})
		if err != nil {
			t.Fatalf("ListArtistsForUser(sort=%s): %v", c.sort, err)
		}
		assertOrder(t, c.sort, artistsOf(got), c.want)
	}
}

func TestPgxTrackRepo_ListArtistsForUser_IlikeMatching(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "n1", artist: "The BEATLES", album: "y1",
		addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "n2", artist: "Rolling Stones", album: "y2",
		addedAt: base.Add(-1 * time.Hour),
	})

	got, err := repo.ListArtistsForUser(ctx, userId, domain.LibraryQuery{Search: "beatles"})
	if err != nil {
		t.Fatalf("ListArtistsForUser(search): %v", err)
	}
	if len(got) != 1 || got[0].Artist != "The BEATLES" {
		t.Fatalf("case-insensitive artist search got %v, want [The BEATLES]", artistsOf(got))
	}
}
