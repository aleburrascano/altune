package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"encoding/json"
	"log/slog"
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
			repo := catalogtest.NewTrackRepo()
			for i := 0; i < tt.seedCount; i++ {
				repo.Seed(makeTrack(testUserId, "Track "+string(rune('A'+i)), "Artist", "Album"))
			}
			_, router := buildTrackHandler(repo, nil)

			rec := serve(t, router, http.MethodGet, "/tracks"+tt.query, nil)

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
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, nil)

	rec := serveNoAuth(t, router, http.MethodGet, "/tracks", nil)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleCreateTrack(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		seedDedup  bool
		wantStatus int
		wantField  string
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
			repo := catalogtest.NewTrackRepo()
			if tt.seedDedup {
				repo.Seed(makeTrack(testUserId, "Existing", "Artist", ""))
			}
			_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

			var rec *httptest.ResponseRecorder
			switch b := tt.body.(type) {
			case string:
				rec2 := serve(t, router, http.MethodPost, "/tracks", strings.NewReader(b))
				rec = rec2
			default:
				rec = serve(t, router, http.MethodPost, "/tracks", jsonBody(t, b))
			}

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
			repo := catalogtest.NewTrackRepo()
			trackId := tt.trackIdFn(repo)
			_, router := buildTrackHandler(repo, nil)

			rec := serve(t, router, http.MethodDelete, "/tracks/"+trackId, nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

// TestHandleDeleteTrack_LogsActor pins #1052: the delete-attempt line names
// the user who triggered it, not just the track.
func TestHandleDeleteTrack_LogsActor(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := catalogtest.NewTrackRepo()
	track := makeTrack(testUserId, "To Delete", "Artist", "Album")
	repo.Seed(track)
	_, router := buildTrackHandler(repo, nil)

	assertStatus(t, serve(t, router, http.MethodDelete, "/tracks/"+track.ID.UUID().String(), nil), http.StatusNoContent)

	for _, r := range ring.Snapshot() {
		if r.Message != "track.delete" {
			continue
		}
		if r.Attrs["user_id"] != testUserId.String() || r.Attrs["track_id"] != track.ID.String() {
			t.Fatalf("track.delete attrs = %v, want user_id %s and track_id %s", r.Attrs, testUserId, track.ID)
		}
		return
	}
	t.Fatal("no track.delete log line")
}

// TestHandleCreateTrack_NulByteRejected is the reproducing case from #2194: a
// U+0000 in any text field of POST /tracks used to travel into a Postgres text
// column, which refuses it with "invalid byte sequence" — an error with no HTTP
// status, so the caller saw a 500. Each field below is accepted without the NUL
// elsewhere in this file, so the 400 belongs to the NUL and not to the field.
func TestHandleCreateTrack_NulByteRejected(t *testing.T) {
	withNul := "a\x00b"
	tests := []struct {
		name string
		body CreateTrackRequest
	}{
		{"title", CreateTrackRequest{Title: withNul, Artist: "Artist"}},
		{"artist", CreateTrackRequest{Title: "Title", Artist: withNul}},
		{"album", CreateTrackRequest{Title: "Title", Artist: "Artist", Album: &withNul}},
		{"genre", CreateTrackRequest{Title: "Title", Artist: "Artist", Genre: &withNul}},
		{"isrc", CreateTrackRequest{Title: "Title", Artist: "Artist", ISRC: &withNul}},
		{
			"featured artist name",
			CreateTrackRequest{
				Title:           "Title",
				Artist:          "Artist",
				FeaturedArtists: []service.FeaturedArtistDTO{{Name: withNul}},
			},
		},
		{
			"source url",
			CreateTrackRequest{
				Title:     "Title",
				Artist:    "Artist",
				SourceURL: strPtr("https://example.com/" + withNul),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := buildTrackHandler(catalogtest.NewTrackRepo(), &catalogtest.Scheduler{})

			rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, tt.body))

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
}

// TestHandleCreateTrack_NulByteInIdempotencyKeyRejected covers the one #2194
// field that arrives as a header rather than in the body. The key is persisted
// on the row, so it is refused before the insert like every other text field.
func TestHandleCreateTrack_NulByteInIdempotencyKeyRejected(t *testing.T) {
	_, router := buildTrackHandler(catalogtest.NewTrackRepo(), &catalogtest.Scheduler{})
	body := CreateTrackRequest{Title: "Title", Artist: "Artist"}
	req := httptest.NewRequest(http.MethodPost, "/tracks", jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake-token")
	req.Header.Set("Idempotency-Key", "a\x00b")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusBadRequest)
}

func TestHandleCreateTrack_ResponseShape(t *testing.T) {
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

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

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

func TestHandleCreateTrackPersistsTrackNumber(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

	trackNumber := 7
	body := CreateTrackRequest{
		Title:       "Dreams",
		Artist:      "Fleetwood Mac",
		Album:       strPtr("Rumours"),
		TrackNumber: &trackNumber,
	}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

	assertStatus(t, rec, http.StatusCreated)

	var resp TrackResponse
	decodeJSON(t, rec, &resp)
	if resp.TrackNumber == nil {
		t.Fatal("track_number was dropped: the client sends it on every album-context save")
	}
	if *resp.TrackNumber != trackNumber {
		t.Errorf("TrackNumber = %d, want %d", *resp.TrackNumber, trackNumber)
	}
}

func TestHandleCreateTrackOmitsTrackNumberWhenAbsent(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	_, router := buildTrackHandler(repo, &catalogtest.Scheduler{})

	body := CreateTrackRequest{Title: "Single", Artist: "Artist"}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

	assertStatus(t, rec, http.StatusCreated)

	var resp TrackResponse
	decodeJSON(t, rec, &resp)
	if resp.TrackNumber != nil {
		t.Errorf("TrackNumber = %d, want nil for a track saved outside an album context", *resp.TrackNumber)
	}
}

func strPtr(s string) *string { return &s }

// TestHandleSetTrackNumber pins the write-once contract at the HTTP edge: both
// the first fill and the silent no-op answer 204, and the handler uses the
// service's updated result to log which one happened.
func TestHandleSetTrackNumber(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := catalogtest.NewTrackRepo()
	track := makeTrack(testUserId, "Dreams", "Fleetwood Mac", "Rumours")
	repo.Seed(track)
	_, router := buildTrackHandler(repo, nil)
	path := "/tracks/" + track.ID.UUID().String() + "/track-number"

	rec := serve(t, router, http.MethodPatch, path, jsonBody(t, SetTrackNumberRequest{TrackNumber: 3}))
	assertStatus(t, rec, http.StatusNoContent)

	rec = serve(t, router, http.MethodPatch, path, jsonBody(t, SetTrackNumberRequest{TrackNumber: 9}))
	assertStatus(t, rec, http.StatusNoContent)

	stored, ok := repo.Tracks[track.ID.String()]
	if !ok || stored == nil || stored.TrackNumber == nil || *stored.TrackNumber != 3 {
		t.Fatal("track number must be 3 after the first fill and stay 3 after the second")
	}

	var events []string
	for _, r := range ring.Snapshot() {
		if strings.HasPrefix(r.Message, "track.track_number_") {
			events = append(events, r.Message)
		}
	}
	want := []string{"track.track_number_set", "track.track_number_unchanged"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("logged events = %v, want %v", events, want)
	}
}

// TestHandleSetTrackNumber_NotFound pins #1049: a track that does not exist,
// or exists but is owned by another user, answers 404 rather than the 204 of
// the write-once no-op, and a foreign track is left untouched. Both cases share
// one 404 so the endpoint does not reveal that a foreign track id exists.
func TestHandleSetTrackNumber_NotFound(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	foreign := makeTrack(shared.NewUserId(uuid.New()), "Theirs", "Artist", "Album")
	repo.Seed(foreign)
	_, router := buildTrackHandler(repo, nil)

	tests := []struct{ name, trackId string }{
		{"nonexistent track", uuid.New().String()},
		{"foreign track", foreign.ID.UUID().String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, router, http.MethodPatch, "/tracks/"+tt.trackId+"/track-number",
				jsonBody(t, SetTrackNumberRequest{TrackNumber: 4}))
			assertStatus(t, rec, http.StatusNotFound)
		})
	}
	if foreign.TrackNumber != nil {
		t.Fatalf("foreign track number = %d, want it left unset", *foreign.TrackNumber)
	}
}
