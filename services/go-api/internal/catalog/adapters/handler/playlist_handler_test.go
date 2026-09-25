package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	catdomain "altune/go-api/internal/catalog/domain"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestHandleCreatePlaylist(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantName   string
	}{
		{
			name:       "valid name returns 201",
			body:       CreatePlaylistRequest{Name: "My Favorites"},
			wantStatus: http.StatusCreated,
			wantName:   "My Favorites",
		},
		{
			name:       "empty name returns 400",
			body:       CreatePlaylistRequest{Name: ""},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "whitespace-only name returns 400",
			body:       CreatePlaylistRequest{Name: " \t\n "},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "surrounding whitespace is trimmed from the stored name",
			body:       CreatePlaylistRequest{Name: "  My Favorites\t"},
			wantStatus: http.StatusCreated,
			wantName:   "My Favorites",
		},
		{
			name:       "invalid JSON returns 400",
			body:       nil,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			_, router := buildPlaylistHandler(plRepo, trRepo)

			var rec *httptest.ResponseRecorder
			if tt.body == nil {
				rec = serve(t, router, http.MethodPost, "/playlists", strings.NewReader("{invalid"))
			} else {
				rec = serve(t, router, http.MethodPost, "/playlists", jsonBody(t, tt.body))
			}

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusCreated {
				var resp PlaylistResponse
				decodeJSON(t, rec, &resp)
				if resp.Name != tt.wantName {
					t.Errorf("Name = %q, want %q", resp.Name, tt.wantName)
				}
				if resp.ID == uuid.Nil {
					t.Error("expected non-nil playlist ID")
				}
				if resp.TrackCount != 0 {
					t.Errorf("TrackCount = %d, want 0 for new playlist", resp.TrackCount)
				}
			}
		})
	}
}

func TestHandleListPlaylists(t *testing.T) {
	tests := []struct {
		name         string
		seedCount    int
		wantStatus   int
		wantItemsLen int
	}{
		{
			name:         "returns all playlists",
			seedCount:    2,
			wantStatus:   http.StatusOK,
			wantItemsLen: 2,
		},
		{
			name:         "empty returns empty array",
			seedCount:    0,
			wantStatus:   http.StatusOK,
			wantItemsLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			for i := 0; i < tt.seedCount; i++ {
				plRepo.Seed(makePlaylist(testUserId, "Playlist "+string(rune('A'+i))))
			}
			_, router := buildPlaylistHandler(plRepo, trRepo)

			rec := serve(t, router, http.MethodGet, "/playlists", nil)

			assertStatus(t, rec, tt.wantStatus)
			assertJSON(t, rec)

			var body httputil.List[PlaylistResponse]
			decodeJSON(t, rec, &body)
			if len(body.Items) != tt.wantItemsLen {
				t.Errorf("len(Items) = %d, want %d", len(body.Items), tt.wantItemsLen)
			}
			if body.Total != tt.wantItemsLen {
				t.Errorf("Total = %d, want %d", body.Total, tt.wantItemsLen)
			}
		})
	}
}

// #1708: the list served every playlist a user owned in one response, so the
// payload grew without bound with the collection. It serves a window now, and a
// caller that names none gets the default page rather than everything.
func TestHandleListPlaylistsServesOneWindow(t *testing.T) {
	const seeded = 60

	tests := []struct {
		name         string
		query        string
		wantItemsLen int
	}{
		{name: "no limit serves the default page", query: "", wantItemsLen: 50},
		{name: "limit and offset serve that window", query: "?limit=10&offset=55", wantItemsLen: 5},
		{name: "an offset past the end serves nothing", query: "?offset=60", wantItemsLen: 0},
		{name: "an unparseable limit falls back to the default page", query: "?limit=abc", wantItemsLen: 50},
		{name: "a limit of zero falls back to the default page", query: "?limit=0", wantItemsLen: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := seededPlaylistRepo(seeded)
			_, router := buildPlaylistHandler(plRepo, catalogtest.NewTrackRepo())

			rec := serve(t, router, http.MethodGet, "/playlists"+tt.query, nil)

			assertStatus(t, rec, http.StatusOK)
			var body httputil.List[PlaylistResponse]
			decodeJSON(t, rec, &body)
			if len(body.Items) != tt.wantItemsLen {
				t.Errorf("len(Items) = %d, want %d of %d seeded", len(body.Items), tt.wantItemsLen, seeded)
			}
		})
	}
}

func TestHandleListPlaylistsPagesWithoutRepeatingARow(t *testing.T) {
	const seeded = 60
	plRepo := seededPlaylistRepo(seeded)
	_, router := buildPlaylistHandler(plRepo, catalogtest.NewTrackRepo())

	first := listedPlaylistIds(t, router, "")
	second := listedPlaylistIds(t, router, "?offset=50")

	seen := map[uuid.UUID]bool{}
	for _, id := range append(first, second...) {
		if seen[id] {
			t.Errorf("playlist %s served by both pages", id)
		}
		seen[id] = true
	}
	if len(seen) != seeded {
		t.Errorf("the two pages covered %d playlists, want all %d", len(seen), seeded)
	}
}

func TestHandleListPlaylistsCapsAnOversizedLimit(t *testing.T) {
	plRepo := seededPlaylistRepo(catdomain.MaxLibraryPageSize + 1)
	_, router := buildPlaylistHandler(plRepo, catalogtest.NewTrackRepo())

	got := listedPlaylistIds(t, router, "?limit=999999")

	if len(got) != catdomain.MaxLibraryPageSize {
		t.Errorf("len(Items) = %d, want the %d row cap", len(got), catdomain.MaxLibraryPageSize)
	}
}

func TestHandleListPlaylistsRejectsANegativeOffset(t *testing.T) {
	plRepo := seededPlaylistRepo(1)
	_, router := buildPlaylistHandler(plRepo, catalogtest.NewTrackRepo())

	rec := serve(t, router, http.MethodGet, "/playlists?offset=-1", nil)

	assertStatus(t, rec, http.StatusBadRequest)
}

func seededPlaylistRepo(count int) *catalogtest.PlaylistRepo {
	repo := catalogtest.NewPlaylistRepo()
	for i := 0; i < count; i++ {
		repo.Seed(makePlaylist(testUserId, "Playlist "+strconv.Itoa(i)))
	}
	return repo
}

func listedPlaylistIds(t *testing.T, router chi.Router, query string) []uuid.UUID {
	t.Helper()
	rec := serve(t, router, http.MethodGet, "/playlists"+query, nil)
	assertStatus(t, rec, http.StatusOK)
	var body httputil.List[PlaylistResponse]
	decodeJSON(t, rec, &body)
	ids := make([]uuid.UUID, len(body.Items))
	for i, item := range body.Items {
		ids[i] = item.ID
	}
	return ids
}

func TestHandleGetPlaylist(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo, *catalogtest.TrackRepo) string
		wantStatus int
	}{
		{
			name: "found returns detail with tracks",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) string {
				pl := makePlaylist(testUserId, "Rock")
				track := makeTrack(testUserId, "Track", "Artist", "Album")
				plRepo.SeedWithTracks(pl, []*catdomain.Track{track})
				return pl.ID.UUID().String()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid ID returns 400",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo, trRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			rec := serve(t, router, http.MethodGet, "/playlists/"+playlistId, nil)

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				var resp PlaylistDetailResponse
				decodeJSON(t, rec, &resp)
				if resp.Name != "Rock" {
					t.Errorf("Name = %q, want %q", resp.Name, "Rock")
				}
				if len(resp.Tracks) != 1 {
					t.Errorf("len(Tracks) = %d, want 1", len(resp.Tracks))
				}
				if resp.TrackCount != 1 {
					t.Errorf("TrackCount = %d, want 1", resp.TrackCount)
				}
			}
		})
	}
}

func TestHandleDeletePlaylist(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo) string
		wantStatus int
	}{
		{
			name: "existing returns 204",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				pl := makePlaylist(testUserId, "To Delete")
				repo.Seed(pl)
				return pl.ID.UUID().String()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found returns 404",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid ID returns 400",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				return "bad-id"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			rec := serve(t, router, http.MethodDelete, "/playlists/"+playlistId, nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleRenamePlaylist(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo) string
		body       any
		wantStatus int
	}{
		{
			name: "valid rename returns 200",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				pl := makePlaylist(testUserId, "Old Name")
				repo.Seed(pl)
				return pl.ID.UUID().String()
			},
			body:       RenamePlaylistRequest{Name: "New Name"},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				return uuid.New().String()
			},
			body:       RenamePlaylistRequest{Name: "New Name"},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "empty name returns 400",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				pl := makePlaylist(testUserId, "Has Name")
				repo.Seed(pl)
				return pl.ID.UUID().String()
			},
			body:       RenamePlaylistRequest{Name: ""},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "whitespace-only name returns 400",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				pl := makePlaylist(testUserId, "Has Name")
				repo.Seed(pl)
				return pl.ID.UUID().String()
			},
			body:       RenamePlaylistRequest{Name: "   \t "},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "surrounding whitespace is trimmed on rename",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				pl := makePlaylist(testUserId, "Old Name")
				repo.Seed(pl)
				return pl.ID.UUID().String()
			},
			body:       RenamePlaylistRequest{Name: " New Name  "},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			rec := serve(t, router, http.MethodPatch, "/playlists/"+playlistId, jsonBody(t, tt.body))

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				var resp PlaylistResponse
				decodeJSON(t, rec, &resp)
				if resp.Name != "New Name" {
					t.Errorf("Name = %q, want %q", resp.Name, "New Name")
				}
			}
		})
	}
}

func TestHandleAddTrack(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo, *catalogtest.TrackRepo) (string, uuid.UUID)
		wantStatus int
	}{
		{
			name: "valid add returns 204",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				plRepo.Seed(pl)
				track := makeTrack(testUserId, "Track", "Artist", "Album")
				trRepo.Seed(track)
				return pl.ID.UUID().String(), track.ID.UUID()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "duplicate track returns 409 Conflict",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				track := makeTrack(testUserId, "Track", "Artist", "Album")
				trRepo.Seed(track)
				_ = pl.AddTrack(track.ID, time.Now())
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), track.ID.UUID()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "playlist not found returns 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, uuid.UUID) {
				track := makeTrack(testUserId, "Track", "Artist", "Album")
				trRepo.Seed(track)
				return uuid.New().String(), track.ID.UUID()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "track not found returns 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), uuid.New()
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackUUID := tt.setup(plRepo, trRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			body := AddTrackToPlaylistRequest{TrackID: trackUUID}

			rec := serve(t, router, http.MethodPost, "/playlists/"+playlistId+"/tracks", jsonBody(t, body))

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleAddTracks(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*catalogtest.PlaylistRepo, *catalogtest.TrackRepo) (string, []uuid.UUID)
		wantStatus  int
		wantAdded   int
		wantSkipped int
	}{
		{
			name: "valid batch returns the added and skipped counts",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				plRepo.Seed(pl)
				first := makeTrack(testUserId, "First", "Artist", "Album")
				second := makeTrack(testUserId, "Second", "Artist", "Album")
				trRepo.Seed(first)
				trRepo.Seed(second)
				return pl.ID.UUID().String(), []uuid.UUID{first.ID.UUID(), second.ID.UUID()}
			},
			wantStatus: http.StatusOK,
			wantAdded:  2,
		},
		{
			name: "a track already in the playlist is skipped, not a conflict",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				existing := makeTrack(testUserId, "Existing", "Artist", "Album")
				fresh := makeTrack(testUserId, "Fresh", "Artist", "Album")
				trRepo.Seed(existing)
				trRepo.Seed(fresh)
				_ = pl.AddTrack(existing.ID, time.Now())
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), []uuid.UUID{existing.ID.UUID(), fresh.ID.UUID()}
			},
			wantStatus:  http.StatusOK,
			wantAdded:   1,
			wantSkipped: 1,
		},
		{
			name: "an unowned track is skipped, not a 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				plRepo.Seed(pl)
				owned := makeTrack(testUserId, "Owned", "Artist", "Album")
				trRepo.Seed(owned)
				return pl.ID.UUID().String(), []uuid.UUID{owned.ID.UUID(), uuid.New()}
			},
			wantStatus:  http.StatusOK,
			wantAdded:   1,
			wantSkipped: 1,
		},
		{
			name: "empty track_ids returns 400",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), []uuid.UUID{}
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "playlist not found returns 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				track := makeTrack(testUserId, "Track", "Artist", "Album")
				trRepo.Seed(track)
				return uuid.New().String(), []uuid.UUID{track.ID.UUID()}
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackUUIDs := tt.setup(plRepo, trRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			body := AddTracksToPlaylistRequest{TrackIDs: trackUUIDs}

			rec := serve(t, router, http.MethodPost, "/playlists/"+playlistId+"/tracks/batch", jsonBody(t, body))

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus != http.StatusOK {
				return
			}
			var resp AddTracksToPlaylistResponse
			decodeJSON(t, rec, &resp)
			if resp.Added != tt.wantAdded {
				t.Errorf("Added = %d, want %d", resp.Added, tt.wantAdded)
			}
			if resp.Skipped != tt.wantSkipped {
				t.Errorf("Skipped = %d, want %d", resp.Skipped, tt.wantSkipped)
			}
		})
	}
}

func TestHandleRemoveTrack(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo) (string, string)
		wantStatus int
	}{
		{
			name: "existing track returns 204",
			setup: func(repo *catalogtest.PlaylistRepo) (string, string) {
				pl := makePlaylist(testUserId, "My List")
				repo.Seed(pl)
				return pl.ID.UUID().String(), uuid.New().String()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "playlist not found returns 404",
			setup: func(repo *catalogtest.PlaylistRepo) (string, string) {
				return uuid.New().String(), uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid playlist ID returns 400",
			setup: func(repo *catalogtest.PlaylistRepo) (string, string) {
				return "bad-id", uuid.New().String()
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *catalogtest.PlaylistRepo) (string, string) {
				pl := makePlaylist(testUserId, "My List")
				repo.Seed(pl)
				return pl.ID.UUID().String(), "bad-id"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			rec := serve(t, router, http.MethodDelete, "/playlists/"+playlistId+"/tracks/"+trackId, nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleRemoveTracks(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*catalogtest.PlaylistRepo, *catalogtest.TrackRepo) (string, []uuid.UUID)
		wantStatus  int
		wantRemoved int
	}{
		{
			name: "valid batch returns the removed count",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				first := makeTrack(testUserId, "First", "Artist", "Album")
				second := makeTrack(testUserId, "Second", "Artist", "Album")
				trRepo.Seed(first)
				trRepo.Seed(second)
				_ = pl.AddTrack(first.ID, time.Now())
				_ = pl.AddTrack(second.ID, time.Now())
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), []uuid.UUID{first.ID.UUID(), second.ID.UUID()}
			},
			wantStatus:  http.StatusOK,
			wantRemoved: 2,
		},
		{
			name: "a track that is not in the playlist is skipped, not a 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				member := makeTrack(testUserId, "Member", "Artist", "Album")
				trRepo.Seed(member)
				_ = pl.AddTrack(member.ID, time.Now())
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), []uuid.UUID{member.ID.UUID(), uuid.New()}
			},
			wantStatus:  http.StatusOK,
			wantRemoved: 1,
		},
		{
			name: "empty track_ids returns 400",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), []uuid.UUID{}
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "playlist not found returns 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, []uuid.UUID) {
				return uuid.New().String(), []uuid.UUID{uuid.New()}
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackUUIDs := tt.setup(plRepo, trRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			body := RemoveTracksFromPlaylistRequest{TrackIDs: trackUUIDs}

			rec := serve(t, router, http.MethodDelete, "/playlists/"+playlistId+"/tracks", jsonBody(t, body))

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus != http.StatusOK {
				return
			}
			var resp RemoveTracksFromPlaylistResponse
			decodeJSON(t, rec, &resp)
			if resp.Removed != tt.wantRemoved {
				t.Errorf("Removed = %d, want %d", resp.Removed, tt.wantRemoved)
			}
		})
	}

	t.Run("the single-track path still addresses exactly that track", func(t *testing.T) {
		plRepo := catalogtest.NewPlaylistRepo()
		trRepo := catalogtest.NewTrackRepo()
		pl := makePlaylist(testUserId, "My List")
		keep := makeTrack(testUserId, "Keep", "Artist", "Album")
		drop := makeTrack(testUserId, "Drop", "Artist", "Album")
		trRepo.Seed(keep)
		trRepo.Seed(drop)
		_ = pl.AddTrack(keep.ID, time.Now())
		_ = pl.AddTrack(drop.ID, time.Now())
		plRepo.Seed(pl)
		_, router := buildPlaylistHandler(plRepo, trRepo)

		rec := serve(t, router, http.MethodDelete,
			"/playlists/"+pl.ID.UUID().String()+"/tracks/"+drop.ID.UUID().String(), nil)

		assertStatus(t, rec, http.StatusNoContent)
		if len(pl.Tracks) != 1 || pl.Tracks[0].TrackId != keep.ID {
			t.Fatalf("tracks = %v, want only %v", pl.Tracks, keep.ID)
		}
	})
}

func TestHandleReorder(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*catalogtest.PlaylistRepo) string
		body       any
		wantStatus int
	}{
		{
			name: "valid reorder returns 204",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				pl := makePlaylist(testUserId, "My List")
				repo.Seed(pl)
				return pl.ID.UUID().String()
			},
			body:       ReorderTracksRequest{TrackIDs: []uuid.UUID{}},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "playlist not found returns 404",
			setup: func(repo *catalogtest.PlaylistRepo) string {
				return uuid.New().String()
			},
			body:       ReorderTracksRequest{TrackIDs: []uuid.UUID{}},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			rec := serve(t, router, http.MethodPatch, "/playlists/"+playlistId+"/tracks/reorder", jsonBody(t, tt.body))

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

// Both batch routes reject an empty track_ids, and a client that has to tell
// this apart from the route's other 400s reads the code, not the detail text.
func TestPlaylistBatchRoutes_EmptyTrackIDsCarryACode(t *testing.T) {
	plRepo := catalogtest.NewPlaylistRepo()
	playlist := makePlaylist(testUserId, "My List")
	plRepo.Seed(playlist)
	_, router := buildPlaylistHandler(plRepo, catalogtest.NewTrackRepo())
	tracksPath := "/playlists/" + playlist.ID.UUID().String() + "/tracks"

	routes := []struct{ method, path string }{
		{http.MethodPost, tracksPath + "/batch"},
		{http.MethodDelete, tracksPath},
	}
	for _, route := range routes {
		t.Run(route.method, func(t *testing.T) {
			body := jsonBody(t, AddTracksToPlaylistRequest{TrackIDs: []uuid.UUID{}})

			rec := serve(t, router, route.method, route.path, body)

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorCode(t, rec, "catalog.track_ids_required")
		})
	}
}
