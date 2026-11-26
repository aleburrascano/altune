package domain_test

import (
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/requestuser"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	validationCode(t, err)
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
	validationCode(t, err)
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

var codecSeeds = []string{
	"", "%", "%4", "%Z", "%ZZ", "%4G", "%G4", "%e9", "%E9", "100%", "%%41", "a+b", "a b", "+", "%2B", "%20", "~-_.",
	"Sigur Rós", "東京", "🎵 mix", "Caf%C3%A9", "%F0%9F%8E%B5", "a:b/c?d=e&f", "\x00\xff\xc3",
}

func codecCases() []string {
	cases := append([]string{}, codecSeeds...)
	for b := 0; b < 256; b++ {
		c := string([]byte{byte(b)})
		cases = append(cases, c, "%"+c+"0", "%0"+c)
	}
	for r := rune(0x80); r < 0x800; r++ {
		cases = append(cases, string(r))
	}
	for r := rune(0x800); r < 0x10000; r += 0x1000 {
		cases = append(cases, string(r))
	}
	for _, r := range []rune{0x10000, 0x40000, 0x80000, 0xC0000, 0x100000} {
		cases = append(cases, string(r))
	}
	return cases
}

func storedForm(value string) string {
	stored := playback.QueueSource{Kind: "playlist", PlaylistId: value}.String()
	return strings.TrimSuffix(strings.TrimPrefix(stored, "playlist:"), ":")
}

func readableOverJSON(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %q: %v", value, err)
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return out
}

func assertReadMatchesNetURL(t *testing.T, encoded string) {
	t.Helper()
	want, err := url.QueryUnescape(encoded)
	got := sourceRead(t, "search:"+encoded)
	if err != nil {
		if got != nil {
			t.Fatalf("search:%s read as %+v, net/url rejects it: %v", encoded, got, err)
		}
		return
	}
	if got == nil || got.Query != readableOverJSON(t, want) {
		t.Fatalf("search:%s read as %+v, net/url decodes %q", encoded, got, want)
	}
}

func assertCodecMatchesNetURL(t *testing.T, value string) {
	t.Helper()
	if got, want := storedForm(value), url.QueryEscape(value); got != want {
		t.Fatalf("%q stored as %q, net/url escapes it to %q", value, got, want)
	}
	assertReadMatchesNetURL(t, value)
	assertReadMatchesNetURL(t, url.QueryEscape(value))
}

func TestQueueSourceCodecMatchesNetURLForEveryByteAndUTF8Lead(t *testing.T) {
	for _, value := range codecCases() {
		assertCodecMatchesNetURL(t, value)
	}
}

func FuzzQueueSourceCodecMatchesNetURL(f *testing.F) {
	for _, seed := range codecSeeds {
		f.Add(seed)
	}
	f.Fuzz(assertCodecMatchesNetURL)
}

func TestStoredSourceIdsStillReadAsTheSameSource(t *testing.T) {
	for stored, want := range map[string]playback.QueueSource{
		"playlist:7c9e6679-7425-40de-944b-e07fc1f90ae7:Road+trip": {Kind: "playlist", PlaylistId: "7c9e6679-7425-40de-944b-e07fc1f90ae7", Name: "Road trip"},
		"playlist:a%3Ab:Best+of%3A+2019":                          {Kind: "playlist", PlaylistId: "a:b", Name: "Best of: 2019"},
		"playlist:42:Caf%C3%A9+%E6%9D%B1%E4%BA%AC+%F0%9F%8E%B5":   {Kind: "playlist", PlaylistId: "42", Name: "Café 東京 🎵"},
		"search:sigur+r%C3%B3s":                                   {Kind: "search", Query: "sigur rós"},
		"search:AC%2FDC+%26+friends":                              {Kind: "search", Query: "AC/DC & friends"},
		"search:100%25+pure~":                                     {Kind: "search", Query: "100% pure~"},
	} {
		if got := sourceRead(t, stored); got == nil || *got != *dtoOf(want) {
			t.Errorf("stored source_id %q read as %+v, want %+v", stored, got, want)
		}
		if got := want.String(); got != stored {
			t.Errorf("%+v now stores as %q, existing rows hold %q", want, got, stored)
		}
	}
}
