package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	catdomain "altune/go-api/internal/catalog/domain"

	"altune/go-api/internal/catalog/catalogtest"

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
			name:       "invalid JSON returns 400",
			body:       nil,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			var rec *httptest.ResponseRecorder
			if tt.body == nil {
				rec = serve(t, router, http.MethodPost, "/playlists", strings.NewReader("{invalid"))
			} else {
				rec = serve(t, router, http.MethodPost, "/playlists", jsonBody(t, tt.body))
			}

			// Assert
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
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			for i := 0; i < tt.seedCount; i++ {
				plRepo.Seed(makePlaylist(testUserId, "Playlist "+string(rune('A'+i))))
			}
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			rec := serve(t, router, http.MethodGet, "/playlists", nil)

			// Assert
			assertStatus(t, rec, tt.wantStatus)
			assertJSON(t, rec)

			var body ListPlaylistsResponse
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
				track := makeTrack(testUserId, "Song", "Artist", "Album")
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
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo, trRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			rec := serve(t, router, http.MethodGet, "/playlists/"+playlistId, nil)

			// Assert
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
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			rec := serve(t, router, http.MethodDelete, "/playlists/"+playlistId, nil)

			// Assert
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			rec := serve(t, router, http.MethodPatch, "/playlists/"+playlistId, jsonBody(t, tt.body))

			// Assert
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
				track := makeTrack(testUserId, "Song", "Artist", "Album")
				trRepo.Seed(track)
				return pl.ID.UUID().String(), track.ID.UUID()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "duplicate track returns 409 Conflict",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, uuid.UUID) {
				pl := makePlaylist(testUserId, "My List")
				track := makeTrack(testUserId, "Song", "Artist", "Album")
				trRepo.Seed(track)
				_ = pl.AddTrack(track.ID) // already in playlist
				plRepo.Seed(pl)
				return pl.ID.UUID().String(), track.ID.UUID()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "playlist not found returns 404",
			setup: func(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (string, uuid.UUID) {
				track := makeTrack(testUserId, "Song", "Artist", "Album")
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
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackUUID := tt.setup(plRepo, trRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			body := AddTrackToPlaylistRequest{TrackID: trackUUID}

			// Act
			rec := serve(t, router, http.MethodPost, "/playlists/"+playlistId+"/tracks", jsonBody(t, body))

			// Assert
			assertStatus(t, rec, tt.wantStatus)
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
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId, trackId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			rec := serve(t, router, http.MethodDelete, "/playlists/"+playlistId+"/tracks/"+trackId, nil)

			// Assert
			assertStatus(t, rec, tt.wantStatus)
		})
	}
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
			// Arrange
			plRepo := catalogtest.NewPlaylistRepo()
			trRepo := catalogtest.NewTrackRepo()
			playlistId := tt.setup(plRepo)
			_, router := buildPlaylistHandler(plRepo, trRepo)

			// Act
			rec := serve(t, router, http.MethodPatch, "/playlists/"+playlistId+"/tracks/reorder", jsonBody(t, tt.body))

			// Assert
			assertStatus(t, rec, tt.wantStatus)
		})
	}
}
