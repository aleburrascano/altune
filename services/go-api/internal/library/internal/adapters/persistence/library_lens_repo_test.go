package persistence_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

func seedLibraryTrack(t *testing.T, repo *library.PostgresCatalogTrackRepository, userId ids.UserId, spec libraryTrackSpec) *library.Track {
	t.Helper()
	track, err := library.NewTrack(userId, spec.title, spec.artist, spec.album, time.Now)
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



func titlesOf(ts []*library.Track) []string {
	out := make([]string, len(ts))
	for i, tr := range ts {
		out[i] = tr.Title
	}
	return out
}

func assertOrder(t *testing.T, sort library.LibrarySort, got, want []string) {
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

func TestPostgresTrackRepo_ListFilteredForUser_SortOrder(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Alpha", artist: "Artist A", album: "Alpha Album",
		year: new(2000), addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "bravo", artist: "Artist B", album: "Bravo Album",
		year: new(2020), addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Charlie", artist: "Artist C", album: "Charlie Album",
		year: nil, addedAt: base.Add(-1 * time.Hour),
	})

	cases := []struct {
		sort library.LibrarySort
		want []string
	}{
		{library.SortAlphabetical, []string{"Alpha", "bravo", "Charlie"}},
		{library.SortRecent, []string{"Charlie", "bravo", "Alpha"}},
		{library.SortYear, []string{"bravo", "Alpha", "Charlie"}},
	}
	for _, c := range cases {
		got, total, err := repo.ListFilteredForUser(ctx, userId, library.LibraryQuery{Sort: c.sort, Limit: 100})
		if err != nil {
			t.Fatalf("ListFilteredForUser(sort=%s): %v", c.sort, err)
		}
		if total != 3 {
			t.Errorf("sort=%s total = %d, want 3", c.sort, total)
		}
		assertOrder(t, c.sort, titlesOf(got), c.want)
	}
}

func TestPostgresTrackRepo_ListFilteredForUser_IlikeMatching(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

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
		got, total, err := repo.ListFilteredForUser(ctx, userId, library.LibraryQuery{
			Search: c.search, Sort: library.SortRecent, Limit: 100,
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

func albumsOf(gs []library.AlbumGroup) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Album
	}
	return out
}

func TestPostgresTrackRepo_ListAlbumsForUser_SortOrder(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "t1", artist: "Zed", album: "Avenue",
		year: new(1990), addedAt: base.Add(-3 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "t2", artist: "Amy", album: "Zephyr",
		year: new(2010), addedAt: base.Add(-1 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "t3", artist: "Bob", album: "Melody",
		year: nil, addedAt: base.Add(-2 * time.Hour),
	})

	cases := []struct {
		sort library.LibrarySort
		want []string
	}{
		{library.SortAlphabetical, []string{"Avenue", "Melody", "Zephyr"}},
		{library.SortYear, []string{"Zephyr", "Avenue", "Melody"}},
		{library.SortRecent, []string{"Zephyr", "Melody", "Avenue"}},
	}
	for _, c := range cases {
		got, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Sort: c.sort})
		if err != nil {
			t.Fatalf("ListAlbumsForUser(sort=%s): %v", c.sort, err)
		}
		assertOrder(t, c.sort, albumsOf(got), c.want)
	}
}

func TestPostgresTrackRepo_ListAlbumsForUser_RespectsLimit(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

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

	got, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Sort: library.SortRecent, Limit: 1})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(Limit:1): %v", err)
	}
	if len(got) > 1 {
		t.Fatalf("Limit:1 returned %d albums, want <= 1", len(got))
	}
	if len(got) == 1 && got[0].Album != "Album Three" {
		t.Errorf("Limit:1 sort=recent = %q, want Album Three", got[0].Album)
	}

	page2, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Sort: library.SortRecent, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(Limit:1,Offset:1): %v", err)
	}
	if len(page2) == 1 && page2[0].Album != "Album Two" {
		t.Errorf("Limit:1 Offset:1 sort=recent = %q, want Album Two", page2[0].Album)
	}
}

func TestPostgresTrackRepo_ListArtistsForUser_RespectsLimit(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

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

	got, err := repo.ListArtistsForUser(ctx, userId, library.LibraryQuery{Sort: library.SortRecent, Limit: 1})
	if err != nil {
		t.Fatalf("ListArtistsForUser(Limit:1): %v", err)
	}
	if len(got) > 1 {
		t.Fatalf("Limit:1 returned %d artists, want <= 1", len(got))
	}
}

func TestPostgresTrackRepo_ListAlbumsForUser_IlikeMatching(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

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
		albumArtist: new("Various Composers"), addedAt: base.Add(-1 * time.Hour),
	})

	byArtist, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Search: "solo"})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(artist search): %v", err)
	}
	if len(byArtist) != 1 || byArtist[0].Album != "Blue Skies" {
		t.Fatalf("artist search got %v, want [Blue Skies]", albumsOf(byArtist))
	}

	byAlbumArtist, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Search: "composers"})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(album_artist search): %v", err)
	}
	if len(byAlbumArtist) != 1 || byAlbumArtist[0].Album != "Compiled" {
		t.Fatalf("album_artist search got %v, want [Compiled]", albumsOf(byAlbumArtist))
	}

	byAlbum, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Search: "BLUE"})
	if err != nil {
		t.Fatalf("ListAlbumsForUser(album search): %v", err)
	}
	if len(byAlbum) != 1 || byAlbum[0].Album != "Blue Skies" {
		t.Fatalf("case-insensitive album search got %v, want [Blue Skies]", albumsOf(byAlbum))
	}
}

func TestPostgresTrackRepo_LibraryGrouping_CoalescesCanonicalVariants(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Halo", artist: "Beyoncé", album: "I Am... Sasha Fierce",
		addedAt: base.Add(-2 * time.Hour),
	})
	seedLibraryTrack(t, repo, userId, libraryTrackSpec{
		title: "Single Ladies", artist: "  Beyoncé  ", album: "  I Am...   Sasha Fierce  ",
		addedAt: base.Add(-1 * time.Hour),
	})

	albums, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Sort: library.SortRecent})
	if err != nil {
		t.Fatalf("ListAlbumsForUser: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("album groups = %d %v, want 1 (variants must coalesce)", len(albums), albumsOf(albums))
	}
	if albums[0].TrackCount != 2 {
		t.Errorf("album track_count = %d, want 2", albums[0].TrackCount)
	}

	artists, err := repo.ListArtistsForUser(ctx, userId, library.LibraryQuery{Sort: library.SortRecent})
	if err != nil {
		t.Fatalf("ListArtistsForUser: %v", err)
	}
	if len(artists) != 1 {
		t.Fatalf("artist groups = %d %v, want 1 (variants must coalesce)", len(artists), artistsOf(artists))
	}
	if artists[0].TrackCount != 2 {
		t.Errorf("artist track_count = %d, want 2", artists[0].TrackCount)
	}
}

func artistsOf(gs []library.ArtistGroup) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Artist
	}
	return out
}

func TestPostgresTrackRepo_ListArtistsForUser_SortOrder(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

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
		sort library.LibrarySort
		want []string
	}{
		{library.SortAlphabetical, []string{"Alpha Artist", "Mid Artist", "Zeta Artist"}},
		{library.SortRecent, []string{"Zeta Artist", "Mid Artist", "Alpha Artist"}},
	}
	for _, c := range cases {
		got, err := repo.ListArtistsForUser(ctx, userId, library.LibraryQuery{Sort: c.sort})
		if err != nil {
			t.Fatalf("ListArtistsForUser(sort=%s): %v", c.sort, err)
		}
		assertOrder(t, c.sort, artistsOf(got), c.want)
	}
}

func TestPostgresTrackRepo_ListArtistsForUser_IlikeMatching(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

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

	got, err := repo.ListArtistsForUser(ctx, userId, library.LibraryQuery{Search: "beatles"})
	if err != nil {
		t.Fatalf("ListArtistsForUser(search): %v", err)
	}
	if len(got) != 1 || got[0].Artist != "The BEATLES" {
		t.Fatalf("case-insensitive artist search got %v, want [The BEATLES]", artistsOf(got))
	}
}

func TestPostgresTrackRepo_ListFilteredForUser_TotalIsExactOnEveryPage(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	for i, title := range []string{"Moon One", "Sun Two", "Moon Three", "Sun Four", "Moon Five"} {
		seedLibraryTrack(t, repo, userId, libraryTrackSpec{
			title: title, artist: "Artist", album: "Album",
			addedAt: base.Add(time.Duration(-i) * time.Minute),
		})
	}

	cases := []struct {
		name          string
		search        string
		limit, offset int
		wantLen       int
		wantTotal     int
	}{
		{"unfiltered full page", "", 2, 0, 2, 5},
		{"unfiltered short last page", "", 2, 4, 1, 5},
		{"unfiltered past the end", "", 2, 10, 0, 5},
		{"search full page", "moon", 2, 0, 2, 3},
		{"search short last page", "moon", 2, 2, 1, 3},
		{"search past the end", "moon", 2, 10, 0, 3},
		{"search exact page boundary", "moon", 3, 0, 3, 3},
		{"search no match", "nothing", 2, 0, 0, 0},
	}
	for _, c := range cases {
		got, total, err := repo.ListFilteredForUser(ctx, userId, library.LibraryQuery{
			Search: c.search, Sort: library.SortRecent, Limit: c.limit, Offset: c.offset,
		})
		if err != nil {
			t.Fatalf("%s: ListFilteredForUser: %v", c.name, err)
		}
		if len(got) != c.wantLen || total != c.wantTotal {
			t.Errorf("%s: len=%d total=%d, want len=%d total=%d", c.name, len(got), total, c.wantLen, c.wantTotal)
		}
	}
}

const (
	tiedGroupCount = 300
	tiedPageSize   = 7
)

func tiedArtistNames() []string {
	names := make([]string, tiedGroupCount)
	for i := range names {
		names[i] = "Artist " + strconv.Itoa(i)
	}
	return names
}

func TestPostgresTrackRepo_ListAlbumsForUser_PagesCoverEveryTiedGroupOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	addedAt := time.Now().UTC().Truncate(time.Second)
	for i, artist := range tiedArtistNames() {
		seedLibraryTrack(t, repo, userId, libraryTrackSpec{
			title: "t" + strconv.Itoa(i), artist: artist, album: "Greatest Hits",
			year: new(2000), addedAt: addedAt,
		})
	}

	for _, sort := range []library.LibrarySort{library.SortAlphabetical, library.SortYear, library.SortRecent} {
		seen := map[string]int{}
		for offset := 0; offset < tiedGroupCount; offset += tiedPageSize {
			page, err := repo.ListAlbumsForUser(ctx, userId, library.LibraryQuery{Sort: sort, Limit: tiedPageSize, Offset: offset})
			if err != nil {
				t.Fatalf("ListAlbumsForUser(sort=%s, offset=%d): %v", sort, offset, err)
			}
			for _, g := range page {
				seen[g.Key]++
			}
		}
		if len(seen) != tiedGroupCount {
			t.Errorf("sort=%s: pages covered %d groups, want %d", sort, len(seen), tiedGroupCount)
		}
		for key, n := range seen {
			if n != 1 {
				t.Errorf("sort=%s: group %q returned %d times, want 1", sort, key, n)
			}
		}
	}
}

func TestPostgresTrackRepo_ListArtistsForUser_PagesCoverEveryTiedGroupOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := ids.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	addedAt := time.Now().UTC().Truncate(time.Second)
	for i, artist := range tiedArtistNames() {
		seedLibraryTrack(t, repo, userId, libraryTrackSpec{
			title: "t" + strconv.Itoa(i), artist: artist, album: "x" + strconv.Itoa(i), addedAt: addedAt,
		})
	}

	seen := map[string]int{}
	for offset := 0; offset < tiedGroupCount; offset += tiedPageSize {
		page, err := repo.ListArtistsForUser(ctx, userId, library.LibraryQuery{Sort: library.SortRecent, Limit: tiedPageSize, Offset: offset})
		if err != nil {
			t.Fatalf("ListArtistsForUser(offset=%d): %v", offset, err)
		}
		for _, g := range page {
			seen[g.Key]++
		}
	}
	if len(seen) != tiedGroupCount {
		t.Errorf("pages covered %d artists, want %d", len(seen), tiedGroupCount)
	}
	for key, n := range seen {
		if n != 1 {
			t.Errorf("artist %q returned %d times, want 1", key, n)
		}
	}
}

func TestPostgresTrackRepo_ListAlbumsForUser_GroupsACompilationByItsAlbumArtist(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool)
	userId := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	for i, artist := range []string{"Adele", "Drake"} {
		seedLibraryTrack(t, repo, userId, libraryTrackSpec{
			title: "c" + strconv.Itoa(i), artist: artist, album: "Now 100",
			albumArtist: new("Various Artists"), addedAt: base.Add(time.Duration(-i) * time.Minute),
		})
	}

	albums, err := repo.ListAlbumsForUser(context.Background(), userId, library.LibraryQuery{Sort: library.SortRecent})
	if err != nil {
		t.Fatalf("ListAlbumsForUser: %v", err)
	}
	if len(albums) != 1 || albums[0].TrackCount != 2 || albums[0].Artist != "Various Artists" {
		t.Fatalf("albums = %+v, want one Now 100 group of 2 tracks by Various Artists", albums)
	}
}
