package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHandleListTracks(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		seedCount    int
		wantStatus   int
		wantItemsLen int
		wantTotal    int
		wantHasMore  bool
		wantLimit    int
	}{
		{
			name:         "returns tracks with default limit",
			query:        "",
			seedCount:    2,
			wantStatus:   http.StatusOK,
			wantItemsLen: 2,
			wantTotal:    2,
			wantHasMore:  false,
			wantLimit:    50,
		},
		{
			name:         "respects explicit limit and offset",
			query:        "?limit=1&offset=0",
			seedCount:    3,
			wantStatus:   http.StatusOK,
			wantItemsLen: 1,
			wantTotal:    3,
			wantHasMore:  true,
			wantLimit:    1,
		},
		{
			name:         "empty library returns empty items array",
			query:        "",
			seedCount:    0,
			wantStatus:   http.StatusOK,
			wantItemsLen: 0,
			wantTotal:    0,
			wantHasMore:  false,
			wantLimit:    50,
		},
		{
			name:         "invalid limit falls back to default",
			query:        "?limit=-1",
			seedCount:    1,
			wantStatus:   http.StatusOK,
			wantItemsLen: 1,
			wantTotal:    1,
			wantHasMore:  false,
			wantLimit:    50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			repo := catalogtest.NewTrackRepo()
			for i := 0; i < tt.seedCount; i++ {
				repo.Seed(makeTrack(testUserId, "Track "+string(rune('A'+i)), "Artist", "Album"))
			}
			_, router := buildTrackHandler(repo, nil)

			// Act
			rec := serve(t, router, http.MethodGet, "/tracks"+tt.query, nil)

			// Assert
			assertStatus(t, rec, tt.wantStatus)
			assertJSON(t, rec)

			var body ListTracksResponse
			decodeJSON(t, rec, &body)
			if len(body.Items) != tt.wantItemsLen {
				t.Errorf("len(Items) = %d, want %d", len(body.Items), tt.wantItemsLen)
			}
			if body.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", body.Total, tt.wantTotal)
			}
			if body.HasMore != tt.wantHasMore {
				t.Errorf("HasMore = %v, want %v", body.HasMore, tt.wantHasMore)
			}
			if body.Limit != tt.wantLimit {
				t.Errorf("Limit = %d, want %d", body.Limit, tt.wantLimit)
			}
		})
	}
}

func TestHandleListTracks_NoAuth(t *testing.T) {
	// Arrange
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, nil)

	// Act
	rec := serveNoAuth(t, router, http.MethodGet, "/tracks", nil)

	// Assert
	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleCreateTrack(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		seedDedup  bool
		wantStatus int
		wantField  string // JSON field to spot-check in response
	}{
		{
			name: "valid track returns 201 Created",
			body: CreateTrackRequest{
				Title:  "New Track",
				Artist: "New Artist",
			},
			wantStatus: http.StatusCreated,
			wantField:  "New Track",
		},
		{
			name: "dedup hit returns 200 OK",
			body: CreateTrackRequest{
				Title:  "Existing",
				Artist: "Artist",
			},
			seedDedup:  true,
			wantStatus: http.StatusOK,
			wantField:  "Existing",
		},
		{
			name: "missing title returns 400",
			body: CreateTrackRequest{
				Title:  "",
				Artist: "Artist",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing artist returns 400",
			body: CreateTrackRequest{
				Title:  "Track",
				Artist: "",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON returns 400",
			body:       "not json{{{",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			repo := catalogtest.NewTrackRepo()
			if tt.seedDedup {
				repo.Seed(makeTrack(testUserId, "Existing", "Artist", ""))
			}
			_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

			// Act
			var rec *httptest.ResponseRecorder
			switch b := tt.body.(type) {
			case string:
				rec2 := serve(t, router, http.MethodPost, "/tracks", strings.NewReader(b))
				rec = rec2
			default:
				rec = serve(t, router, http.MethodPost, "/tracks", jsonBody(t, b))
			}

			// Assert
			assertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusCreated || tt.wantStatus == http.StatusOK {
				var resp TrackResponse
				decodeJSON(t, rec, &resp)
				if resp.Title != tt.wantField {
					t.Errorf("Title = %q, want %q", resp.Title, tt.wantField)
				}
				if resp.ID == uuid.Nil {
					t.Error("expected non-nil track ID in response")
				}
				if resp.AcquisitionStatus == "" {
					t.Error("expected non-empty acquisition_status in response")
				}
			}
		})
	}
}

func TestHandleDeleteTrack(t *testing.T) {
	tests := []struct {
		name       string
		trackIdFn  func(repo *catalogtest.TrackRepo) string
		wantStatus int
	}{
		{
			name: "existing track returns 204",
			trackIdFn: func(repo *catalogtest.TrackRepo) string {
				track := makeTrack(testUserId, "To Delete", "Artist", "Album")
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "not found returns 404",
			trackIdFn: func(repo *catalogtest.TrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid UUID returns 400",
			trackIdFn: func(repo *catalogtest.TrackRepo) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			repo := catalogtest.NewTrackRepo()
			trackId := tt.trackIdFn(repo)
			_, router := buildTrackHandler(repo, nil)

			// Act
			rec := serve(t, router, http.MethodDelete, "/tracks/"+trackId, nil)

			// Assert
			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestHandleCreateTrack_ResponseShape(t *testing.T) {
	// Arrange
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

	dur := 180.5
	artwork := "https://example.com/art.jpg"
	year := 2024
	genre := "Rock"
	albumArtist := "Various Artists"
	isrc := "USRC12345678"
	body := CreateTrackRequest{
		Title:           "Full Track",
		Artist:          "Full Artist",
		Album:           strPtr("Full Album"),
		DurationSeconds: &dur,
		ArtworkURL:      &artwork,
		Year:            &year,
		Genre:           &genre,
		AlbumArtist:     &albumArtist,
		ISRC:            &isrc,
	}

	// Act
	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

	// Assert
	assertStatus(t, rec, http.StatusCreated)

	var raw map[string]json.RawMessage
	decodeJSON(t, rec, &raw)

	requiredFields := []string{"id", "title", "artist", "added_at", "acquisition_status"}
	for _, f := range requiredFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("response missing required field %q", f)
		}
	}
}

func strPtr(s string) *string { return &s }
