package handler_test

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/httpserver"
	sharedids "altune/go-api/internal/shared/ids"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestHandleCreatePlaylist_IdempotencyKey(t *testing.T) {
	rig := newWriteRig(time.Now)
	user := librarytest.UserId()
	send := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/playlists/", jsonBody(t, map[string]string{"name": "Road trip"}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpserver.IdempotencyKeyHeader, key)
		return sendAs(rig.router, req, user)
	}

	first := send("retry-1")
	again := send("retry-1")
	other := send("retry-2")

	assertStatus(t, first, http.StatusCreated)
	assertStatus(t, again, http.StatusOK)
	assertStatus(t, other, http.StatusCreated)
	var a, b, c struct {
		ID string `json:"id"`
	}
	decodeJSON(t, first, &a)
	decodeJSON(t, again, &b)
	decodeJSON(t, other, &c)
	if a.ID != b.ID {
		t.Errorf("the repeat returned playlist %s, want the first, %s", b.ID, a.ID)
	}
	if c.ID == a.ID {
		t.Error("a different key reused the first playlist")
	}
	assertStatus(t, send("a\x00b"), http.StatusBadRequest)
}

func TestHandleCreatePlaylist(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantName   string
	}{
		{
			name:       "valid name returns 201",
			body:       handler.CreatePlaylistRequest{Name: "My Favorites"},
			wantStatus: http.StatusCreated,
			wantName:   "My Favorites",
		},
		{
			name:       "empty name returns 400",
			body:       handler.CreatePlaylistRequest{Name: ""},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "whitespace-only name returns 400",
			body:       handler.CreatePlaylistRequest{Name: " \t\n "},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "surrounding whitespace is trimmed from the stored name",
			body:       handler.CreatePlaylistRequest{Name: "  My Favorites\t"},
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
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			router := playlistRoutes(plRepo, trRepo)

			var rec *httptest.ResponseRecorder
			if tt.body == nil {
				rec = request(t, router, http.MethodPost, "/playlists", strings.NewReader("{invalid"))
			} else {
				rec = request(t, router, http.MethodPost, "/playlists", jsonBody(t, tt.body))
			}

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusCreated {
				var resp handler.PlaylistResponse
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
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			for i := 0; i < tt.seedCount; i++ {
				librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "Playlist "+string(rune('A'+i)))
			}
			router := playlistRoutes(plRepo, trRepo)

			rec := request(t, router, http.MethodGet, "/playlists", nil)

			assertStatus(t, rec, tt.wantStatus)
			assertJSON(t, rec)

			var body httpserver.List[handler.PlaylistResponse]
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
		{name: "a limit of zero falls back to the default page", query: "?limit=0", wantItemsLen: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := seededPlaylistRepo(t, seeded)
			router := playlistRoutes(plRepo, memory.NewTrackRepo())

			rec := request(t, router, http.MethodGet, "/playlists"+tt.query, nil)

			assertStatus(t, rec, http.StatusOK)
			var body httpserver.List[handler.PlaylistResponse]
			decodeJSON(t, rec, &body)
			if len(body.Items) != tt.wantItemsLen {
				t.Errorf("len(Items) = %d, want %d of %d seeded", len(body.Items), tt.wantItemsLen, seeded)
			}
		})
	}
}

func TestHandleListPlaylistsRejectsAnUnparseableWindow(t *testing.T) {
	for query, wantDetail := range map[string]string{
		"?limit=abc":  "limit must be an integer",
		"?offset=1.5": "offset must be an integer",
	} {
		t.Run(query, func(t *testing.T) {
			router := playlistRoutes(seededPlaylistRepo(t, 3), memory.NewTrackRepo())

			rec := request(t, router, http.MethodGet, "/playlists"+query, nil)

			assertStatus(t, rec, http.StatusBadRequest)
			assertProblem(t, rec, "catalog.validation_error", wantDetail)
		})
	}
}

func TestHandleListPlaylistsPagesWithoutRepeatingARow(t *testing.T) {
	const seeded = 60
	plRepo := seededPlaylistRepo(t, seeded)
	router := playlistRoutes(plRepo, memory.NewTrackRepo())

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
	plRepo := seededPlaylistRepo(t, domain.MaxLibraryPageSize+1)
	router := playlistRoutes(plRepo, memory.NewTrackRepo())

	got := listedPlaylistIds(t, router, "?limit=999999")

	if len(got) != domain.MaxLibraryPageSize {
		t.Errorf("len(Items) = %d, want the %d row cap", len(got), domain.MaxLibraryPageSize)
	}
}

func TestHandleListPlaylistsRejectsANegativeOffset(t *testing.T) {
	plRepo := seededPlaylistRepo(t, 1)
	router := playlistRoutes(plRepo, memory.NewTrackRepo())

	rec := request(t, router, http.MethodGet, "/playlists?offset=-1", nil)

	assertStatus(t, rec, http.StatusBadRequest)
}

func seededPlaylistRepo(t *testing.T, count int) *memory.PlaylistRepo {
	t.Helper()
	repo := memory.NewPlaylistRepo()
	for i := 0; i < count; i++ {
		librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "Playlist "+strconv.Itoa(i))
	}
	return repo
}

func listedPlaylistIds(t *testing.T, router chi.Router, query string) []uuid.UUID {
	t.Helper()
	rec := request(t, router, http.MethodGet, "/playlists"+query, nil)
	assertStatus(t, rec, http.StatusOK)
	var body httpserver.List[handler.PlaylistResponse]
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
		setup      func(*testing.T, *memory.PlaylistRepo, *memory.TrackRepo) string
		wantStatus int
	}{
		{
			name: "found returns detail with tracks",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) string {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "Rock")
				track := librarytest.SeedTrack(t, plRepo.Tracks, librarytest.UserId(), "Track", "Artist", "Album")
				plRepo.SeedWithTracks(t, pl, []*domain.Track{track})
				return pl.ID.UUID().String()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid ID returns 400",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId := tt.setup(t, plRepo, trRepo)
			router := playlistRoutes(plRepo, trRepo)

			rec := request(t, router, http.MethodGet, "/playlists/"+playlistId, nil)

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				var resp handler.PlaylistDetailResponse
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
		setup      func(*testing.T, *memory.PlaylistRepo) string
		wantStatus int
	}{
		{
			name: "existing returns 204",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "To Delete")
				return pl.ID.UUID().String()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found returns 404",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid ID returns 400",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				return "bad-id"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId := tt.setup(t, plRepo)
			router := playlistRoutes(plRepo, trRepo)

			rec := request(t, router, http.MethodDelete, "/playlists/"+playlistId, nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleRenamePlaylist(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*testing.T, *memory.PlaylistRepo) string
		body       any
		wantStatus int
	}{
		{
			name: "valid rename returns 200",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "Old Name")
				return pl.ID.UUID().String()
			},
			body:       handler.RenamePlaylistRequest{Name: "New Name"},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found returns 404",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				return uuid.New().String()
			},
			body:       handler.RenamePlaylistRequest{Name: "New Name"},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "empty name returns 400",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "Has Name")
				return pl.ID.UUID().String()
			},
			body:       handler.RenamePlaylistRequest{Name: ""},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "whitespace-only name returns 400",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "Has Name")
				return pl.ID.UUID().String()
			},
			body:       handler.RenamePlaylistRequest{Name: "   \t "},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "surrounding whitespace is trimmed on rename",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "Old Name")
				return pl.ID.UUID().String()
			},
			body:       handler.RenamePlaylistRequest{Name: " New Name  "},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId := tt.setup(t, plRepo)
			router := playlistRoutes(plRepo, trRepo)

			rec := request(t, router, http.MethodPatch, "/playlists/"+playlistId, jsonBody(t, tt.body))

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				var resp handler.PlaylistResponse
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
		setup      func(*testing.T, *memory.PlaylistRepo, *memory.TrackRepo) (string, uuid.UUID)
		wantStatus int
	}{
		{
			name: "valid add returns 204",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				track := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Track", "Artist", "Album")
				return pl.ID.UUID().String(), track.ID().UUID()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "duplicate track returns 409 Conflict",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				track := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Track", "Artist", "Album")
				_ = pl.AddTrack(track.ID(), time.Now())
				return pl.ID.UUID().String(), track.ID().UUID()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "playlist not found returns 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, uuid.UUID) {
				track := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Track", "Artist", "Album")
				return uuid.New().String(), track.ID().UUID()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "track not found returns 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				return pl.ID.UUID().String(), uuid.New()
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId, trackUUID := tt.setup(t, plRepo, trRepo)
			router := playlistRoutes(plRepo, trRepo)

			body := handler.AddTrackToPlaylistRequest{TrackID: trackUUID}

			rec := request(t, router, http.MethodPost, "/playlists/"+playlistId+"/tracks", jsonBody(t, body))

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleAddTracks(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*testing.T, *memory.PlaylistRepo, *memory.TrackRepo) (string, []uuid.UUID)
		wantStatus  int
		wantAdded   int
		wantSkipped int
	}{
		{
			name: "valid batch returns the added and skipped counts",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				first := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "First", "Artist", "Album")
				second := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Second", "Artist", "Album")
				return pl.ID.UUID().String(), []uuid.UUID{first.ID().UUID(), second.ID().UUID()}
			},
			wantStatus: http.StatusOK,
			wantAdded:  2,
		},
		{
			name: "a track already in the playlist is skipped, not a conflict",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				existing := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Existing", "Artist", "Album")
				fresh := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Fresh", "Artist", "Album")
				_ = pl.AddTrack(existing.ID(), time.Now())
				return pl.ID.UUID().String(), []uuid.UUID{existing.ID().UUID(), fresh.ID().UUID()}
			},
			wantStatus:  http.StatusOK,
			wantAdded:   1,
			wantSkipped: 1,
		},
		{
			name: "an unowned track is skipped, not a 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				owned := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Owned", "Artist", "Album")
				return pl.ID.UUID().String(), []uuid.UUID{owned.ID().UUID(), uuid.New()}
			},
			wantStatus:  http.StatusOK,
			wantAdded:   1,
			wantSkipped: 1,
		},
		{
			name: "empty track_ids returns 400",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				return pl.ID.UUID().String(), []uuid.UUID{}
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "playlist not found returns 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				track := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Track", "Artist", "Album")
				return uuid.New().String(), []uuid.UUID{track.ID().UUID()}
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId, trackUUIDs := tt.setup(t, plRepo, trRepo)
			router := playlistRoutes(plRepo, trRepo)

			body := handler.AddTracksToPlaylistRequest{TrackIDs: trackUUIDs}

			rec := request(t, router, http.MethodPost, "/playlists/"+playlistId+"/tracks/batch", jsonBody(t, body))

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus != http.StatusOK {
				return
			}
			var resp handler.AddTracksToPlaylistResponse
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
		setup      func(*testing.T, *memory.PlaylistRepo) (string, string)
		wantStatus int
	}{
		{
			name: "existing track returns 204",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) (string, string) {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "My List")
				return pl.ID.UUID().String(), uuid.New().String()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "playlist not found returns 404",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) (string, string) {
				return uuid.New().String(), uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid playlist ID returns 400",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) (string, string) {
				return "bad-id", uuid.New().String()
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) (string, string) {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "My List")
				return pl.ID.UUID().String(), "bad-id"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId, trackId := tt.setup(t, plRepo)
			router := playlistRoutes(plRepo, trRepo)

			rec := request(t, router, http.MethodDelete, "/playlists/"+playlistId+"/tracks/"+trackId, nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleRemoveTracks(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*testing.T, *memory.PlaylistRepo, *memory.TrackRepo) (string, []uuid.UUID)
		wantStatus  int
		wantRemoved int
	}{
		{
			name: "valid batch returns the removed count",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				first := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "First", "Artist", "Album")
				second := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Second", "Artist", "Album")
				_ = pl.AddTrack(first.ID(), time.Now())
				_ = pl.AddTrack(second.ID(), time.Now())
				return pl.ID.UUID().String(), []uuid.UUID{first.ID().UUID(), second.ID().UUID()}
			},
			wantStatus:  http.StatusOK,
			wantRemoved: 2,
		},
		{
			name: "a track that is not in the playlist is skipped, not a 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				member := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Member", "Artist", "Album")
				_ = pl.AddTrack(member.ID(), time.Now())
				return pl.ID.UUID().String(), []uuid.UUID{member.ID().UUID(), uuid.New()}
			},
			wantStatus:  http.StatusOK,
			wantRemoved: 1,
		},
		{
			name: "empty track_ids returns 400",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
				return pl.ID.UUID().String(), []uuid.UUID{}
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "playlist not found returns 404",
			setup: func(t *testing.T, plRepo *memory.PlaylistRepo, trRepo *memory.TrackRepo) (string, []uuid.UUID) {
				return uuid.New().String(), []uuid.UUID{uuid.New()}
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId, trackUUIDs := tt.setup(t, plRepo, trRepo)
			router := playlistRoutes(plRepo, trRepo)

			body := handler.RemoveTracksFromPlaylistRequest{TrackIDs: trackUUIDs}

			rec := request(t, router, http.MethodDelete, "/playlists/"+playlistId+"/tracks", jsonBody(t, body))

			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus != http.StatusOK {
				return
			}
			var resp handler.RemoveTracksFromPlaylistResponse
			decodeJSON(t, rec, &resp)
			if resp.Removed != tt.wantRemoved {
				t.Errorf("Removed = %d, want %d", resp.Removed, tt.wantRemoved)
			}
		})
	}

	t.Run("the single-track path still addresses exactly that track", func(t *testing.T) {
		plRepo := memory.NewPlaylistRepo()
		trRepo := memory.NewTrackRepo()
		pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
		keep := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Keep", "Artist", "Album")
		drop := librarytest.SeedTrack(t, trRepo, librarytest.UserId(), "Drop", "Artist", "Album")
		_ = pl.AddTrack(keep.ID(), time.Now())
		_ = pl.AddTrack(drop.ID(), time.Now())
		router := playlistRoutes(plRepo, trRepo)

		rec := request(t, router, http.MethodDelete,
			"/playlists/"+pl.ID.UUID().String()+"/tracks/"+drop.ID().UUID().String(), nil)

		assertStatus(t, rec, http.StatusNoContent)
		if len(pl.Tracks) != 1 || pl.Tracks[0].TrackId != keep.ID() {
			t.Fatalf("tracks = %v, want only %v", pl.Tracks, keep.ID())
		}
	})
}

func TestHandleReorder(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*testing.T, *memory.PlaylistRepo) string
		body       any
		wantStatus int
	}{
		{
			name: "valid reorder returns 204",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				pl := librarytest.SeedPlaylist(t, repo, librarytest.UserId(), "My List")
				return pl.ID.UUID().String()
			},
			body:       handler.ReorderTracksRequest{TrackIDs: []uuid.UUID{}},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "playlist not found returns 404",
			setup: func(t *testing.T, repo *memory.PlaylistRepo) string {
				return uuid.New().String()
			},
			body:       handler.ReorderTracksRequest{TrackIDs: []uuid.UUID{}},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plRepo := memory.NewPlaylistRepo()
			trRepo := memory.NewTrackRepo()
			playlistId := tt.setup(t, plRepo)
			router := playlistRoutes(plRepo, trRepo)

			rec := request(t, router, http.MethodPatch, "/playlists/"+playlistId+"/tracks/reorder", jsonBody(t, tt.body))

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestPlaylistBatchRoutes_EmptyTrackIDsCarryACode(t *testing.T) {
	plRepo := memory.NewPlaylistRepo()
	playlist := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "My List")
	router := playlistRoutes(plRepo, memory.NewTrackRepo())
	tracksPath := "/playlists/" + playlist.ID.UUID().String() + "/tracks"

	routes := []struct{ method, path string }{
		{http.MethodPost, tracksPath + "/batch"},
		{http.MethodDelete, tracksPath},
	}
	for _, route := range routes {
		t.Run(route.method, func(t *testing.T) {
			body := jsonBody(t, handler.AddTracksToPlaylistRequest{TrackIDs: []uuid.UUID{}})

			rec := request(t, router, route.method, route.path, body)

			assertStatus(t, rec, http.StatusBadRequest)
			assertErrorCode(t, rec, "catalog.track_ids_required")
		})
	}
}

func TestHandleGetPlaylist_EncodesAfterHugeDurationsSubmitted(t *testing.T) {
	trRepo := memory.NewTrackRepo()
	trackRouter := trackRoutesWith(trRepo, trackDeps{scheduler: &memory.Scheduler{}})
	for _, title := range []string{"Dreams", "Rhiannon"} {
		body := `{"title":"` + title + `","artist":"Fleetwood Mac","duration_seconds":1.7976931348623157e308}`
		request(t, trackRouter, http.MethodPost, "/tracks", strings.NewReader(body))
	}
	stored := make([]*domain.Track, 0, len(trRepo.Tracks))
	for _, track := range trRepo.Tracks {
		stored = append(stored, track)
	}
	plRepo := memory.NewPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "Long")
	plRepo.SeedWithTracks(t, pl, stored)
	playlistRouter := playlistRoutes(plRepo, trRepo)

	rec := request(t, playlistRouter, http.MethodGet, "/playlists/"+pl.ID.UUID().String(), nil)

	assertStatus(t, rec, http.StatusOK)
	var resp handler.PlaylistDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("playlist detail is not valid JSON (%v): %q", err, rec.Body.String())
	}
}

func TestPlaylistWrites_ThrottlePerUser(t *testing.T) {
	clock := newFakeClock()
	rig := newThrottledWriteRig(clock.now)
	noisy, quiet := sharedids.NewUserId(uuid.New()), sharedids.NewUserId(uuid.New())
	playlistId, trackId := rig.seedPlaylistAndTrack(t, noisy)

	if rec := rig.createPlaylist(t, noisy, "First"); rec.Code != http.StatusCreated {
		t.Fatalf("create inside the burst must store, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := rig.addTrackToPlaylist(t, noisy, playlistId, trackId); rec.Code != http.StatusNoContent {
		t.Fatalf("add inside the burst must store, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := rig.addTracksToPlaylist(t, noisy, playlistId, trackId); rec.Code != http.StatusOK {
		t.Fatalf("batch add inside the burst must be served, got %d (%s)", rec.Code, rec.Body.String())
	}
	for i := range playlistWriteBurst - 3 {
		if rec := rig.createPlaylist(t, noisy, "Filler "+strconv.Itoa(i)); rec.Code != http.StatusCreated {
			t.Fatalf("create %d inside the burst must store, got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}

	assertWriteThrottled(t, rig.createPlaylist(t, noisy, "Past the burst"), "1")
	assertWriteThrottled(t, rig.addTrackToPlaylist(t, noisy, playlistId, trackId), "1")
	assertWriteThrottled(t, rig.addTracksToPlaylist(t, noisy, playlistId, trackId), "1")

	if rec := rig.createPlaylist(t, quiet, "Untouched"); rec.Code != http.StatusCreated {
		t.Fatalf("another user's write budget must be untouched, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestPlaylistWrites_LeaveReadsAndRemovalsUnthrottled(t *testing.T) {
	clock := newFakeClock()
	rig := newThrottledWriteRig(clock.now)
	user := sharedids.NewUserId(uuid.New())
	playlistId, trackId := rig.seedPlaylistAndTrack(t, user)

	if rec := rig.addTrackToPlaylist(t, user, playlistId, trackId); rec.Code != http.StatusNoContent {
		t.Fatalf("the budgeted add must store, got %d (%s)", rec.Code, rec.Body.String())
	}
	for i := range playlistWriteBurst - 1 {
		if rec := rig.createPlaylist(t, user, "Filler "+strconv.Itoa(i)); rec.Code != http.StatusCreated {
			t.Fatalf("create %d inside the burst must store, got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	assertWriteThrottled(t, rig.addTrackToPlaylist(t, user, playlistId, trackId), "1")

	req := httptest.NewRequest(http.MethodGet, "/playlists/", nil)
	req.Header.Set("Authorization", "Bearer "+user.String())
	list := httptest.NewRecorder()
	rig.router.ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list with a spent write budget = %d, want 200 (%s)", list.Code, list.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/playlists/"+playlistId.String()+"/tracks/"+trackId.String(), nil)
	req.Header.Set("Authorization", "Bearer "+user.String())
	remove := httptest.NewRecorder()
	rig.router.ServeHTTP(remove, req)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove with a spent write budget = %d, want 204 (%s)", remove.Code, remove.Body.String())
	}
}

func TestHandleListPlaylists_ReportsEachPlaylistsTrackCount(t *testing.T) {
	plRepo := memory.NewPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "Rock")
	plRepo.SeedWithTracks(t, pl, []*domain.Track{
		librarytest.SeedTrack(t, plRepo.Tracks, librarytest.UserId(), "One", "Artist", "Album"),
		librarytest.SeedTrack(t, plRepo.Tracks, librarytest.UserId(), "Two", "Artist", "Album"),
	})
	router := playlistRoutes(plRepo, memory.NewTrackRepo())

	rec := request(t, router, http.MethodGet, "/playlists", nil)

	assertStatus(t, rec, http.StatusOK)
	var body httpserver.List[handler.PlaylistResponse]
	decodeJSON(t, rec, &body)
	if len(body.Items) != 1 || body.Items[0].TrackCount != 2 {
		t.Fatalf("listed = %+v, want one playlist with track_count 2", body.Items)
	}
}

func TestHandleCreatePlaylist_SendsArtworkAsAnEmptyArrayNotNull(t *testing.T) {
	router := playlistRoutes(memory.NewPlaylistRepo(), memory.NewTrackRepo())

	rec := request(t, router, http.MethodPost, "/playlists", jsonBody(t, handler.CreatePlaylistRequest{Name: "Empty"}))

	assertStatus(t, rec, http.StatusCreated)
	if !strings.Contains(rec.Body.String(), `"preview_artwork_urls":[]`) {
		t.Fatalf("body = %s, want preview_artwork_urls as [] so clients can iterate it", rec.Body.String())
	}
}

func TestHandleGetPlaylist_SumsTheTrackDurations(t *testing.T) {
	plRepo := memory.NewPlaylistRepo()
	pl := librarytest.SeedPlaylist(t, plRepo, librarytest.UserId(), "Timed")
	first, second := 100.5, 200.0
	one := librarytest.SeedTrack(t, plRepo.Tracks, librarytest.UserId(), "One", "Artist", "Album")
	librarytest.AmendTrack(one, func(s *librarytest.TrackSnapshot) { s.DurationSeconds = &first })
	two := librarytest.SeedTrack(t, plRepo.Tracks, librarytest.UserId(), "Two", "Artist", "Album")
	librarytest.AmendTrack(two, func(s *librarytest.TrackSnapshot) { s.DurationSeconds = &second })
	untimed := librarytest.SeedTrack(t, plRepo.Tracks, librarytest.UserId(), "Untimed", "Artist", "Album")
	plRepo.SeedWithTracks(t, pl, []*domain.Track{one, two, untimed})
	router := playlistRoutes(plRepo, memory.NewTrackRepo())

	rec := request(t, router, http.MethodGet, "/playlists/"+pl.ID.UUID().String(), nil)

	assertStatus(t, rec, http.StatusOK)
	var resp handler.PlaylistDetailResponse
	decodeJSON(t, rec, &resp)
	if resp.TotalDurationSeconds != 300.5 {
		t.Fatalf("total_duration_seconds = %v, want 300.5", resp.TotalDurationSeconds)
	}
}
