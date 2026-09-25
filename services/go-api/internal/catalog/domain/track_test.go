package domain

import (
	"altune/go-api/internal/shared"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseTrackId(t *testing.T) {
	t.Parallel()
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantStr string
	}{
		{
			name:    "valid UUID",
			input:   validUUID,
			wantErr: false,
			wantStr: validUUID,
		},
		{
			name:    "invalid UUID",
			input:   "not-a-uuid",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, err := ParseTrackId(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := id.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestTrackIdFromUUID(t *testing.T) {
	t.Parallel()
	raw := uuid.New()
	id := TrackIdFromUUID(raw)

	if id.UUID() != raw {
		t.Errorf("UUID() = %v, want %v", id.UUID(), raw)
	}
	if id.String() != raw.String() {
		t.Errorf("String() = %q, want %q", id.String(), raw.String())
	}
}

func TestTrackId_IsZero(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   TrackId
		want bool
	}{
		{
			name: "nil UUID is zero",
			id:   TrackIdFromUUID(uuid.Nil),
			want: true,
		},
		{
			name: "non-nil UUID is not zero",
			id:   TrackIdFromUUID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")),
			want: false,
		},
		{
			name: "default value is zero",
			id:   TrackId{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.id.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAcquisitionStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    AcquisitionStatus
		wantErr bool
	}{
		{name: "pending", input: "pending", want: AcquisitionPending},
		{name: "ready", input: "ready", want: AcquisitionReady},
		{name: "failed", input: "failed", want: AcquisitionFailed},
		{name: "invalid value", input: "downloading", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAcquisitionStatus(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseAcquisitionStatus(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAcquisitionStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status AcquisitionStatus
		want   string
	}{
		{name: "pending", status: AcquisitionPending, want: "pending"},
		{name: "ready", status: AcquisitionReady, want: "ready"},
		{name: "failed", status: AcquisitionFailed, want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewTrack(t *testing.T) {
	t.Parallel()
	userId := shared.NewUserId(uuid.New())

	tests := []struct {
		name      string
		title     string
		artist    string
		album     string
		wantAlbum string
		wantErr   string
	}{
		{
			name:      "valid with all fields",
			title:     "Track Title",
			artist:    "Artist Name",
			album:     "Album Name",
			wantAlbum: "Album Name",
		},
		{
			name:      "empty album falls back to the title (a single)",
			title:     "Track Title",
			artist:    "Artist Name",
			album:     "",
			wantAlbum: "Track Title",
		},
		{
			name:      "blank album falls back to the title",
			title:     "Track Title",
			artist:    "Artist Name",
			album:     "   ",
			wantAlbum: "Track Title",
		},
		{
			name:    "empty title returns error",
			title:   "",
			artist:  "Artist Name",
			album:   "Album Name",
			wantErr: "track title required",
		},
		{
			name:    "empty artist returns error",
			title:   "Track Title",
			artist:  "",
			album:   "Album Name",
			wantErr: "track artist required",
		},
		{
			name:    "whitespace-only title returns error",
			title:   "   ",
			artist:  "Artist Name",
			album:   "Album Name",
			wantErr: "track title required",
		},
		{
			name:    "whitespace-only artist returns error",
			title:   "Track Title",
			artist:  "\t\n ",
			album:   "Album Name",
			wantErr: "track artist required",
		},
		{
			name:    "overlong title returns error",
			title:   strings.Repeat("a", 301),
			artist:  "Artist Name",
			album:   "Album Name",
			wantErr: "track title exceeds 300 characters",
		},
		{
			name:    "overlong artist returns error",
			title:   "Track Title",
			artist:  strings.Repeat("b", 301),
			album:   "Album Name",
			wantErr: "track artist exceeds 300 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track, err := NewTrack(userId, tt.title, tt.artist, tt.album)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				if track != nil {
					t.Error("expected nil track on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track.ID.IsZero() {
				t.Error("expected non-zero ID")
			}
			if track.UserId != userId {
				t.Errorf("UserId = %v, want %v", track.UserId, userId)
			}
			if track.Title != tt.title {
				t.Errorf("Title = %q, want %q", track.Title, tt.title)
			}
			if track.Artist != tt.artist {
				t.Errorf("Artist = %q, want %q", track.Artist, tt.artist)
			}
			if track.Album != tt.wantAlbum {
				t.Errorf("Album = %q, want %q", track.Album, tt.wantAlbum)
			}
			if track.AcquisitionStatus != AcquisitionPending {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionPending", track.AcquisitionStatus)
			}
			if track.DedupKey == "" {
				t.Error("expected non-empty DedupKey")
			}
			if track.DedupKey != computeDedupKey(tt.title, tt.artist, tt.wantAlbum) {
				t.Errorf("DedupKey = %q, want %q", track.DedupKey, computeDedupKey(tt.title, tt.artist, tt.wantAlbum))
			}
			if track.AddedAt.IsZero() {
				t.Error("expected non-zero AddedAt")
			}
		})
	}
}

func TestNewTrack_StoresTrimmedTitleAndArtist(t *testing.T) {
	t.Parallel()
	userId := shared.NewUserId(uuid.New())

	track, err := NewTrack(userId, "  Track Title  ", "\tArtist Name\n", "Album Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track.Title != "Track Title" {
		t.Errorf("Title = %q, want trimmed %q", track.Title, "Track Title")
	}
	if track.Artist != "Artist Name" {
		t.Errorf("Artist = %q, want trimmed %q", track.Artist, "Artist Name")
	}
	if want := computeDedupKey("Track Title", "Artist Name", "Album Name"); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
}

// TestNewTrack_CanonicalizesAlbumAndArtistForGrouping reproduces issue #432:
// two tracks whose album/artist differ only by stray whitespace or Unicode form
// must be stored with identical album/artist so the library-lens grouping (which
// applies SQL lower()) coalesces them into one group instead of fragmenting,
// matching dedup's notion of equivalence.
func TestNewTrack_CanonicalizesAlbumAndArtistForGrouping(t *testing.T) {
	t.Parallel()
	userId := shared.NewUserId(uuid.New())

	// NFC vs NFKD "Beyoncé": precomposed é vs e + combining acute accent. NFKC
	// folds both to the same form; a raw store would split them into two groups.
	clean, err := NewTrack(userId, "Halo", "Beyoncé", "I Am... Sasha Fierce")
	if err != nil {
		t.Fatalf("NewTrack(clean): %v", err)
	}
	variant, err := NewTrack(userId, "Other Song", "  Beyoncé  ", "  I Am...   Sasha Fierce  ")
	if err != nil {
		t.Fatalf("NewTrack(variant): %v", err)
	}

	if clean.Artist != variant.Artist {
		t.Errorf("artist not coalesced: clean %q vs variant %q", clean.Artist, variant.Artist)
	}
	if clean.Album != variant.Album {
		t.Errorf("album not coalesced: clean %q vs variant %q", clean.Album, variant.Album)
	}
	// Display form is preserved: case and punctuation survive canonicalization.
	if clean.Album != "I Am... Sasha Fierce" {
		t.Errorf("album display mangled: %q", clean.Album)
	}
}

// TestSetAlbumArtist_Canonicalizes covers the album_artist write path used by
// AddTrackService: whitespace/Unicode variants canonicalize to one value and an
// all-whitespace value clears the field.
func TestSetAlbumArtist_Canonicalizes(t *testing.T) {
	t.Parallel()
	userId := shared.NewUserId(uuid.New())
	track, err := NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}

	track.SetAlbumArtist("  Various   Artists  ")
	if track.AlbumArtist == nil || *track.AlbumArtist != "Various Artists" {
		t.Errorf("SetAlbumArtist = %v, want canonical %q", track.AlbumArtist, "Various Artists")
	}

	track.SetAlbumArtist("   ")
	if track.AlbumArtist != nil {
		t.Errorf("blank album_artist should clear the field, got %v", track.AlbumArtist)
	}
}

func TestTrack_MarkReady(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		audioRef string
		wantErr  string
	}{
		{
			name:     "valid audioRef sets status and AudioRef",
			audioRef: "s3://bucket/track.opus",
		},
		{
			name:     "empty audioRef returns error",
			audioRef: "",
			wantErr:  "audio_ref required for ready status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := newTestTrack(t)
			reason := "old failure"
			track.FailureReason = &reason

			err := track.MarkReady(tt.audioRef)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track.AcquisitionStatus != AcquisitionReady {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionReady", track.AcquisitionStatus)
			}
			if track.AudioRef == nil || *track.AudioRef != tt.audioRef {
				t.Errorf("AudioRef = %v, want %q", track.AudioRef, tt.audioRef)
			}
			if track.FailureReason != nil {
				t.Errorf("FailureReason should be nil after MarkReady, got %q", *track.FailureReason)
			}
			if track.AudioVersion == "" {
				t.Error("a ready track must carry a non-empty audio version")
			}
		})
	}
}

func TestTrack_AudioVersion(t *testing.T) {
	t.Parallel()

	t.Run("is empty until audio has been written", func(t *testing.T) {
		t.Parallel()
		track := newTestTrack(t)

		if track.AudioVersion != "" {
			t.Errorf("AudioVersion = %q, want empty for a track with no audio", track.AudioVersion)
		}
	})

	t.Run("changes on every re-acquisition, even when the audio ref is byte-identical", func(t *testing.T) {
		t.Parallel()
		track := newTestTrack(t)
		const sameRef = "s3://bucket/track.opus"

		seen := make(map[string]bool)
		for i := 0; i < 100; i++ {
			if err := track.MarkReady(sameRef); err != nil {
				t.Fatalf("acquisition %d: %v", i, err)
			}
			if seen[track.AudioVersion] {
				t.Fatalf("AudioVersion %q repeated on acquisition %d — a client keyed on it would keep the old audio", track.AudioVersion, i)
			}
			seen[track.AudioVersion] = true
		}
	})

	t.Run("survives a round trip through MarkFailed and back", func(t *testing.T) {
		t.Parallel()
		track := newTestTrack(t)

		if err := track.MarkReady("s3://bucket/track.opus"); err != nil {
			t.Fatalf("first acquisition: %v", err)
		}
		first := track.AudioVersion

		if err := track.MarkFailed("download_failed"); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
		if err := track.MarkReady("s3://bucket/track.opus"); err != nil {
			t.Fatalf("retry: %v", err)
		}

		if track.AudioVersion == first {
			t.Error("a retry after a failure must still publish a fresh version")
		}
	})
}

func TestTrack_MarkFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reason  string
		wantErr string
	}{
		{
			name:   "valid reason sets status and FailureReason",
			reason: "download timeout",
		},
		{
			name:    "empty reason returns error",
			reason:  "",
			wantErr: "failure_reason required for failed status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := newTestTrack(t)
			ref := "s3://bucket/track.opus"
			track.AudioRef = &ref

			err := track.MarkFailed(tt.reason)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if track.AcquisitionStatus != AcquisitionFailed {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionFailed", track.AcquisitionStatus)
			}
			if track.FailureReason == nil || *track.FailureReason != tt.reason {
				t.Errorf("FailureReason = %v, want %q", track.FailureReason, tt.reason)
			}
			if track.AudioRef != nil {
				t.Errorf("AudioRef should be nil after MarkFailed, got %q", *track.AudioRef)
			}
		})
	}
}

func TestTrack_RevertToPending(t *testing.T) {
	t.Parallel()
	track := newTestTrack(t)

	if err := track.MarkReady("s3://bucket/track.opus"); err != nil {
		t.Fatalf("setup: MarkReady failed: %v", err)
	}

	if err := track.RevertToPending(); err != nil {
		t.Fatalf("RevertToPending: %v", err)
	}

	if track.AcquisitionStatus != AcquisitionPending {
		t.Errorf("AcquisitionStatus = %v, want AcquisitionPending", track.AcquisitionStatus)
	}
	if track.AudioRef != nil {
		t.Errorf("AudioRef should be nil after RevertToPending, got %q", *track.AudioRef)
	}
	if track.FailureReason != nil {
		t.Errorf("FailureReason should be nil after RevertToPending, got %q", *track.FailureReason)
	}
}

func TestTrack_IsStreamable(t *testing.T) {
	t.Parallel()
	audioRef := "s3://bucket/track.opus"

	tests := []struct {
		name  string
		setup func(*Track)
		want  bool
	}{
		{
			name: "ready with audioRef is streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionReady
				tr.AudioRef = &audioRef
			},
			want: true,
		},
		{
			name: "ready without audioRef is not streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionReady
				tr.AudioRef = nil
			},
			want: false,
		},
		{
			name: "pending is not streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionPending
			},
			want: false,
		},
		{
			name: "failed is not streamable",
			setup: func(tr *Track) {
				tr.AcquisitionStatus = AcquisitionFailed
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := newTestTrack(t)
			tt.setup(track)
			if got := track.IsStreamable(); got != tt.want {
				t.Errorf("IsStreamable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestTrack(t *testing.T) *Track {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := NewTrack(userId, "Test Title", "Test Artist", "Test Album")
	if err != nil {
		t.Fatalf("newTestTrack: unexpected error: %v", err)
	}
	return track
}

func TestSetAlbum_KeepsTheDedupKeyInStep(t *testing.T) {
	t.Parallel()
	userId := shared.NewUserId(uuid.New())
	track, err := NewTrack(userId, "Crook", "Raf Saperra", "Some Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	track.SetAlbum("Other Album")

	if track.Album != "Other Album" {
		t.Errorf("Album = %q, want %q", track.Album, "Other Album")
	}
	if want := computeDedupKey("Crook", "Raf Saperra", "Other Album"); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
}

func TestSetAlbum_BlankFallsBackToTheTitle(t *testing.T) {
	t.Parallel()
	track := &Track{Title: "Crook", Artist: "Raf Saperra", Album: ""}

	track.SetAlbum("")

	if track.Album != "Crook" {
		t.Errorf("Album = %q, want the title", track.Album)
	}
	if want := computeDedupKey("Crook", "Raf Saperra", "Crook"); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
}

// TestTrackTextLengthMessages pins the exact "exceeds N characters" wording at
// the optional-field and source_url call sites (title/artist are pinned in
// TestNewTrack), so building the message from maxTrackTextLength cannot drift
// the text callers see.
func TestTrackTextLengthMessages(t *testing.T) {
	t.Parallel()
	overlong := strings.Repeat("x", 301)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"optional album", ValidateOptionalTrackText(&overlong, "album"), "track album exceeds 300 characters"},
		{"optional genre", ValidateOptionalTrackText(&overlong, "genre"), "track genre exceeds 300 characters"},
		{"source url", ValidateSourceURL("https://example.com/" + overlong), "track source_url exceeds 300 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err == nil || tt.err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", tt.err, tt.want)
			}
		})
	}
}

// TestNulByteRefusedByEveryTextEntryPoint is the regression guard for #2194. A
// U+0000 used to travel unchecked into a Postgres text column, which refuses it
// with "invalid byte sequence" — a driver error carrying no HTTP status, so the
// request answered 500 and logged service.unhandled_error. Each entry point
// below must instead refuse it as a 400, and must still accept the same text
// once the NUL is gone.
func TestNulByteRefusedByEveryTextEntryPoint(t *testing.T) {
	t.Parallel()
	userId := shared.NewUserId(uuid.New())
	newTrackErr := func(title, artist, album string) error {
		_, err := NewTrack(userId, title, artist, album)
		return err
	}
	tests := []struct {
		name     string
		validate func(text string) error
		want     string
	}{
		{
			"track title",
			func(s string) error { return newTrackErr(s, "Artist", "Album") },
			"track title must not contain a NUL byte",
		},
		{
			"track artist",
			func(s string) error { return newTrackErr("Title", s, "Album") },
			"track artist must not contain a NUL byte",
		},
		{
			"track album",
			func(s string) error { return newTrackErr("Title", "Artist", s) },
			"track album must not contain a NUL byte",
		},
		{
			"optional track text",
			func(s string) error { return ValidateOptionalTrackText(&s, "genre") },
			"track genre must not contain a NUL byte",
		},
		{
			"playlist name",
			func(s string) error {
				_, err := NewPlaylist(userId, s, time.Unix(0, 0))
				return err
			},
			"playlist name must not contain a NUL byte",
		},
		{
			"featured artist name",
			func(s string) error { return ValidateFeaturedArtist(FeaturedArtist{Name: s}) },
			"track featured_artists name must not contain a NUL byte",
		},
		{
			"featured artist mbid",
			func(s string) error { return ValidateFeaturedArtist(FeaturedArtist{MBID: s}) },
			"track featured_artists mbid must not contain a NUL byte",
		},
		{
			"source url",
			func(s string) error { return ValidateSourceURL("https://example.com/" + s) },
			"track source_url is malformed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.validate("a\x00b")
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error for NUL input = %v, want %q", err, tt.want)
			}
			var invalid *ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("error %v is not a *ValidationError, so it answers 500 not 400", err)
			}
			if got := invalid.HTTPStatus(); got != 400 {
				t.Errorf("HTTPStatus() = %d, want 400", got)
			}
			if err := tt.validate("ab"); err != nil {
				t.Errorf("same text without the NUL was rejected: %v", err)
			}
		})
	}
}

func markerUserId(t *testing.T) shared.UserId {
	t.Helper()
	return shared.NewUserId(uuid.New())
}

func TestNewTrack_SetsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if track.AcquisitionStatus != AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.AcquisitionStatus)
	}
	if track.AcquisitionStartedAt == nil {
		t.Fatal("AcquisitionStartedAt = nil, want the in-flight marker set at creation")
	}
	if !track.AcquisitionStartedAt.Equal(track.AddedAt) {
		t.Errorf("AcquisitionStartedAt = %v, want it to equal AddedAt %v", track.AcquisitionStartedAt, track.AddedAt)
	}
}

func TestTrack_MarkReady_ClearsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkReady("s3://bucket/audio.opus"); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if track.AcquisitionStartedAt != nil {
		t.Errorf("AcquisitionStartedAt = %v, want nil after MarkReady", track.AcquisitionStartedAt)
	}
}

func TestTrack_MarkFailed_ClearsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed(string(FailureAcquisitionInterrupted)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if track.AcquisitionStartedAt != nil {
		t.Errorf("AcquisitionStartedAt = %v, want nil after MarkFailed", track.AcquisitionStartedAt)
	}
}

func TestTrack_RevertToPending_RefreshesInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := NewTrack(markerUserId(t), "Title", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed("download_failed"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if err := track.RevertToPending(); err != nil {
		t.Fatalf("RevertToPending: %v", err)
	}

	if track.AcquisitionStatus != AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.AcquisitionStatus)
	}
	if track.AcquisitionStartedAt == nil {
		t.Fatal("AcquisitionStartedAt = nil, want a fresh marker after RevertToPending")
	}
}

func TestFailureMessage_AcquisitionInterrupted(t *testing.T) {
	t.Parallel()
	reason := string(FailureAcquisitionInterrupted)
	if got := FailureMessage(&reason); got != "Acquisition was interrupted" {
		t.Errorf("FailureMessage = %q, want %q", got, "Acquisition was interrupted")
	}
}

// Both codes are persisted in failure_reason and published in the
// track_acquisition_failed payload, so renaming a value strands every stored
// row and shipped client that already carries the old one.
func TestAcquisitionFailureCodes_KeepTheirStoredValues(t *testing.T) {
	t.Parallel()
	stored := []struct {
		code FailureCode
		want string
	}{
		{FailureAcquisitionInterrupted, "acquisition_interrupted"},
		{FailureAcquisitionRefused, "acquisition_refused"},
	}
	for _, s := range stored {
		if string(s.code) != s.want {
			t.Errorf("stored failure code = %q, want %q", s.code, s.want)
		}
	}
}

func TestTrackSetDuration_RejectsUnstorableValues(t *testing.T) {
	t.Parallel()
	for name, seconds := range map[string]float64{
		"+Inf":      math.Inf(1),
		"-Inf":      math.Inf(-1),
		"NaN":       math.NaN(),
		"negative":  -1,
		"above cap": MaxDurationSeconds + 1,
		"max float": math.MaxFloat64,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			track := &Track{}

			err := track.SetDuration(seconds)

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("SetDuration(%v) error = %v, want a ValidationError", seconds, err)
			}
			if track.DurationSeconds != nil {
				t.Errorf("DurationSeconds = %v, want unset", *track.DurationSeconds)
			}
		})
	}
}

func TestTrackSetDuration_StoresPositiveAndIgnoresZero(t *testing.T) {
	t.Parallel()
	track := &Track{}

	if err := track.SetDuration(0); err != nil || track.DurationSeconds != nil {
		t.Fatalf("SetDuration(0) = %v, duration %v; want nil error and unset", err, track.DurationSeconds)
	}
	if err := track.SetDuration(MaxDurationSeconds); err != nil {
		t.Fatalf("SetDuration(max) error = %v", err)
	}
	if track.DurationSeconds == nil || *track.DurationSeconds != MaxDurationSeconds {
		t.Errorf("DurationSeconds = %v, want %d", track.DurationSeconds, MaxDurationSeconds)
	}
}

// With every stored duration at most the cap, a playlist total stays finite
// and therefore JSON-encodable.
func TestTotalDurationSeconds_FiniteAtCap(t *testing.T) {
	t.Parallel()
	tracks := make([]*Track, 10000)
	for i := range tracks {
		tracks[i] = &Track{}
		if err := tracks[i].SetDuration(MaxDurationSeconds); err != nil {
			t.Fatal(err)
		}
	}

	if total := TotalDurationSeconds(tracks); math.IsInf(total, 0) || math.IsNaN(total) {
		t.Errorf("TotalDurationSeconds = %v, want finite", total)
	}
}

const transitionRef = "s3://bucket/track.opus"

type trackState func(t *testing.T) *Track

func pendingTrack(t *testing.T) *Track {
	t.Helper()
	return newTestTrack(t)
}

func readyTrack(t *testing.T) *Track {
	t.Helper()
	track := newTestTrack(t)
	if err := track.MarkReady(transitionRef); err != nil {
		t.Fatalf("setup MarkReady: %v", err)
	}
	return track
}

func failedTrack(t *testing.T) *Track {
	t.Helper()
	track := newTestTrack(t)
	if err := track.MarkFailed("download_failed"); err != nil {
		t.Fatalf("setup MarkFailed: %v", err)
	}
	return track
}

// readyWithoutAudioTrack is a ready row missing its audio_ref, the broken
// state cmd/backfillaudio repairs with MarkReady.
func readyWithoutAudioTrack(t *testing.T) *Track {
	t.Helper()
	track := newTestTrack(t)
	track.AcquisitionStatus = AcquisitionReady
	track.AcquisitionStartedAt = nil
	return track
}

var transitions = map[string]func(*Track) error{
	"MarkReady":        func(tr *Track) error { return tr.MarkReady("s3://bucket/other.opus") },
	"MarkReadySameRef": func(tr *Track) error { return tr.MarkReady(transitionRef) },
	"ReplaceAudio":     func(tr *Track) error { return tr.ReplaceAudio("s3://bucket/other.opus") },
	"MarkFailed":       func(tr *Track) error { return tr.MarkFailed("audio file missing from storage") },
	"FailAcquisition":  func(tr *Track) error { return tr.FailAcquisition("no_match_found") },
	"RevertToPending":  func(tr *Track) error { return tr.RevertToPending() },
}

func TestTrack_IllegalAcquisitionTransitionsAreRefusedAndLeaveTheTrackUnchanged(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		from   trackState
		method string
	}{
		{"MarkReady on a ready track under a different ref", readyTrack, "MarkReady"},
		{"ReplaceAudio on a pending track", pendingTrack, "ReplaceAudio"},
		{"ReplaceAudio on a failed track", failedTrack, "ReplaceAudio"},
		{"MarkFailed on a failed track (duplicate failure)", failedTrack, "MarkFailed"},
		{"FailAcquisition on a ready track (stale failure after success)", readyTrack, "FailAcquisition"},
		{"FailAcquisition on a failed track", failedTrack, "FailAcquisition"},
		{"RevertToPending on a pending track", pendingTrack, "RevertToPending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			track := tc.from(t)
			before := *track

			err := transitions[tc.method](track)

			if !errors.Is(err, ErrIllegalAcquisitionTransition) {
				t.Fatalf("%s error = %v, want ErrIllegalAcquisitionTransition", tc.method, err)
			}
			if !reflect.DeepEqual(*track, before) {
				t.Errorf("track changed by a refused transition:\n got %+v\nwant %+v", *track, before)
			}
		})
	}
}

func TestTrack_LegalAcquisitionTransitionsSucceed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		from   trackState
		method string
		want   AcquisitionStatus
	}{
		{"MarkReady from pending (acquisition completed)", pendingTrack, "MarkReady", AcquisitionReady},
		{"MarkReady from failed (late success, backfill)", failedTrack, "MarkReady", AcquisitionReady},
		{"MarkReady on ready without audio (backfill repair)", readyWithoutAudioTrack, "MarkReady", AcquisitionReady},
		{"MarkReady on ready under the same ref (duplicate completion rewrote it)", readyTrack, "MarkReadySameRef", AcquisitionReady},
		{"ReplaceAudio on a ready track", readyTrack, "ReplaceAudio", AcquisitionReady},
		{"MarkFailed from pending (refused schedule, stale sweep)", pendingTrack, "MarkFailed", AcquisitionFailed},
		{"MarkFailed from ready (audio missing from storage)", readyTrack, "MarkFailed", AcquisitionFailed},
		{"FailAcquisition from pending", pendingTrack, "FailAcquisition", AcquisitionFailed},
		{"RevertToPending from ready (re-acquire)", readyTrack, "RevertToPending", AcquisitionPending},
		{"RevertToPending from failed (retry)", failedTrack, "RevertToPending", AcquisitionPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			track := tc.from(t)

			if err := transitions[tc.method](track); err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if track.AcquisitionStatus != tc.want {
				t.Errorf("status = %v, want %v", track.AcquisitionStatus, tc.want)
			}
		})
	}
}
