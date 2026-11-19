package domain_test

import (
	"altune/go-api/internal/playback"
	shareddomain "altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/requestuser"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type sourceDTO struct {
	Kind       string `json:"kind"`
	PlaylistId string `json:"playlist_id"`
	Name       string `json:"name"`
	Query      string `json:"query"`
}

func formatSource(source playback.QueueSource, fallback string) (string, error) {
	saved, err := saveQueue(playback.SaveQueueStateInput{Source: source, LegacySourceId: fallback})
	if err != nil {
		return "", err
	}
	return saved.SourceId, nil
}

func sourceRead(t *testing.T, storedSourceId string) *sourceDTO {
	t.Helper()
	user := testUser()
	store := &storeSpy{stored: &playback.QueueState{UserId: user, TrackIds: []string{}, NaturalOrder: []string{}, SourceId: storedSourceId}}
	routes := playback.NewQueueHandler(playback.NewQueueService(store, nil)).Routes()
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), user))
	rec := httptest.NewRecorder()

	routes.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /queue-state = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Source *sourceDTO `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode queue state: %v", err)
	}
	return body.Source
}

func dtoOf(source playback.QueueSource) *sourceDTO {
	return &sourceDTO{Kind: source.Kind, PlaylistId: source.PlaylistId, Name: source.Name, Query: source.Query}
}

func TestQueueSource_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		source playback.QueueSource
	}{
		{"library", playback.QueueSource{Kind: "library"}},
		{"playlist", playback.QueueSource{Kind: "playlist", PlaylistId: "abc", Name: "Road trip"}},
		{"playlist name with colon", playback.QueueSource{Kind: "playlist", PlaylistId: "abc", Name: "Best of: 2019"}},
		{"playlist id with colon", playback.QueueSource{Kind: "playlist", PlaylistId: "a:b", Name: "x"}},
		{"playlist id with colon no name", playback.QueueSource{Kind: "playlist", PlaylistId: "a:b:c"}},
		{"playlist id with percent", playback.QueueSource{Kind: "playlist", PlaylistId: "a%b", Name: "y"}},
		{"search", playback.QueueSource{Kind: "search", Query: "boards of canada"}},
		{"search without query", playback.QueueSource{Kind: "search"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := formatSource(tt.source, "")
			if err != nil {
				t.Fatalf("save %+v: %v", tt.source, err)
			}
			if stored != tt.source.String() {
				t.Errorf("stored source_id = %q, want %q", stored, tt.source.String())
			}

			if got, want := sourceRead(t, stored), dtoOf(tt.source); *got != *want {
				t.Errorf("round trip = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseQueueSource_UnreadableTokensReadAsNoSource(t *testing.T) {
	for name, token := range map[string]string{
		"empty":                     "",
		"unknown kind":              "mixtape:7",
		"malformed search":          "search:%ZZ",
		"malformed playlist id":     "playlist:%ZZ:name",
		"malformed playlist name":   "playlist:abc:%ZZ",
		"idless playlist":           "playlist:",
		"idless playlist, no name":  "playlist::",
		"idless playlist with name": "playlist::Road+trip",
	} {
		t.Run(name, func(t *testing.T) {
			if got := sourceRead(t, token); got != nil {
				t.Errorf("stored source_id %q read back as %+v, want no source", token, got)
			}
		})
	}
}

func TestQueueSource_StringZero(t *testing.T) {
	if got := (playback.QueueSource{}).String(); got != "" {
		t.Errorf("String() = %q, want empty", got)
	}
}

func TestFormatQueueSource_UnknownKindIsValidationError(t *testing.T) {
	_, err := formatSource(playback.QueueSource{Kind: "album", PlaylistId: "xyz"}, "album:xyz")
	if err == nil {
		t.Fatal("expected an unknown non-empty kind to be rejected, got nil error")
	}
	var ve *shareddomain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestFormatQueueSource_IdlessPlaylistStoresNoSourceInsteadOfFailing(t *testing.T) {
	for name, source := range map[string]playback.QueueSource{
		"no id":       {Kind: "playlist"},
		"no id, name": {Kind: "playlist", Name: "Road trip"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := formatSource(source, "")
			if err != nil {
				t.Fatalf("a playlist source naming no playlist must not fail the save: %v", err)
			}
			if got != "" {
				t.Errorf("formatSource(%+v) = %q, want no stored source", source, got)
			}
		})
	}
}

func TestFormatQueueSource_IdlessPlaylistFallbackStoresNoSource(t *testing.T) {
	for _, fallback := range []string{"playlist:", "playlist::", "playlist::Road+trip"} {
		got, err := formatSource(playback.QueueSource{}, fallback)
		if err != nil {
			t.Errorf("legacy source_id %q naming no playlist must not fail the save: %v", fallback, err)
			continue
		}
		if got != "" {
			t.Errorf("formatSource(zero, %q) = %q, want no stored source", fallback, got)
		}
	}
}

func TestFormatQueueSource_PlaylistWithIdFormats(t *testing.T) {
	got, err := formatSource(playback.QueueSource{Kind: "playlist", PlaylistId: "abc", Name: "Road trip"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "playlist:abc:Road+trip" {
		t.Errorf("formatSource(playlist) = %q, want %q", got, "playlist:abc:Road+trip")
	}
}

func TestFormatQueueSource_KnownKindFormats(t *testing.T) {
	got, err := formatSource(playback.QueueSource{Kind: "library"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "library" {
		t.Errorf("formatSource(library) = %q, want %q", got, "library")
	}
}

func TestFormatQueueSource_ZeroSourceFallsBackToSourceId(t *testing.T) {
	got, err := formatSource(playback.QueueSource{}, "search:legacy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "search:legacy" {
		t.Errorf("formatSource(zero) = %q, want fallback %q", got, "search:legacy")
	}
}

func TestFormatQueueSource_EmptyFallbackStaysEmpty(t *testing.T) {
	got, err := formatSource(playback.QueueSource{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("formatSource(zero, \"\") = %q, want empty", got)
	}
}

func TestFormatQueueSource_GarbageFallbackIsValidationError(t *testing.T) {
	_, err := formatSource(playback.QueueSource{}, "mixtape:7")
	if err == nil {
		t.Fatal("expected garbage legacy source_id to be rejected, got nil error")
	}
	var ve *shareddomain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestFormatQueueSource_EachRejectionHasItsOwnCode(t *testing.T) {
	tests := []struct {
		name     string
		source   playback.QueueSource
		fallback string
		wantCode string
	}{
		{
			name:     "unknown source kind",
			source:   playback.QueueSource{Kind: "album", PlaylistId: "xyz"},
			fallback: "album:xyz",
			wantCode: "playback.unknown_source_kind",
		},
		{
			name:     "undecodable legacy source_id",
			fallback: "mixtape:7",
			wantCode: "playback.unrecognized_source_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := formatSource(tt.source, tt.fallback)

			if got := validationCode(t, err); got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestFormatQueueSource_LegitimateFallbacksAccepted(t *testing.T) {
	for _, fallback := range []string{
		"library",
		"search",
		"search:boards of canada",
		"playlist:abc:Road trip",
		"playlist:a%3Ab:x",
	} {
		got, err := formatSource(playback.QueueSource{}, fallback)
		if err != nil {
			t.Errorf("legitimate fallback %q rejected: %v", fallback, err)
			continue
		}
		if got != fallback {
			t.Errorf("legitimate fallback %q rewritten to %q", fallback, got)
		}
	}
}

func TestFormatQueueSource_EveryAcceptedKindFormatsToNonEmptyToken(t *testing.T) {
	for _, kind := range []string{"library", "playlist", "search"} {
		got, err := formatSource(playback.QueueSource{Kind: kind, PlaylistId: "abc"}, "")
		if err != nil {
			t.Fatalf("formatSource(%q) error: %v", kind, err)
		}
		if got == "" {
			t.Errorf("formatSource(%q) = empty token", kind)
		}
	}
}
