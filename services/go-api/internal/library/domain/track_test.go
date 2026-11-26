package domain_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"altune/go-api/internal/shared/tracklength"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const maxDurationSeconds = 7 * 24 * 60 * 60

func TestParseAcquisitionStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    library.DownloadStatus
		wantErr bool
	}{
		{name: "pending", input: "pending", want: library.AcquisitionPending},
		{name: "ready", input: "ready", want: library.AcquisitionReady},
		{name: "failed", input: "failed", want: library.AcquisitionFailed},
		{name: "invalid value", input: "downloading", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := library.ParseAcquisitionStatus(tt.input)
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
		status library.DownloadStatus
		want   string
	}{
		{name: "pending", status: library.AcquisitionPending, want: "pending"},
		{name: "ready", status: library.AcquisitionReady, want: "ready"},
		{name: "failed", status: library.AcquisitionFailed, want: "failed"},
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
	userId := ids.NewUserId(uuid.New())

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
		{
			name:    "overlong album returns error",
			title:   "Track Title",
			artist:  "Artist Name",
			album:   strings.Repeat("c", 301),
			wantErr: "track album exceeds 300 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track, err := library.NewTrack(userId, tt.title, tt.artist, tt.album, time.Now)
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
			if track.DownloadStatus != library.AcquisitionPending {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionPending", track.DownloadStatus)
			}
			if track.DedupKey == "" {
				t.Error("expected non-empty DedupKey")
			}
			if track.DedupKey != library.TrackDedupKey(tt.title, tt.artist, tt.wantAlbum) {
				t.Errorf("DedupKey = %q, want %q", track.DedupKey, library.TrackDedupKey(tt.title, tt.artist, tt.wantAlbum))
			}
			if track.AddedAt.IsZero() {
				t.Error("expected non-zero AddedAt")
			}
		})
	}
}

func TestNewTrack_AcceptsAlbumAtCap(t *testing.T) {
	t.Parallel()
	album := strings.Repeat("c", 300)

	track, err := library.NewTrack(ids.NewUserId(uuid.New()), "Track Title", "Artist Name", album, time.Now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track.Album != album {
		t.Errorf("Album length = %d, want 300", len(track.Album))
	}
}

func TestNewTrack_StoresTrimmedTitleAndArtist(t *testing.T) {
	t.Parallel()
	userId := ids.NewUserId(uuid.New())

	track, err := library.NewTrack(userId, "  Track Title  ", "\tArtist Name\n", "Album Name", time.Now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track.Title != "Track Title" {
		t.Errorf("Title = %q, want trimmed %q", track.Title, "Track Title")
	}
	if track.Artist != "Artist Name" {
		t.Errorf("Artist = %q, want trimmed %q", track.Artist, "Artist Name")
	}
	if want := library.TrackDedupKey("Track Title", "Artist Name", "Album Name"); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
}

func TestNewTrack_CanonicalizesAlbumAndArtistForGrouping(t *testing.T) {
	t.Parallel()
	userId := ids.NewUserId(uuid.New())

	clean, err := library.NewTrack(userId, "Halo", "Beyoncé", "I Am... Sasha Fierce", time.Now)
	if err != nil {
		t.Fatalf("NewTrack(clean): %v", err)
	}
	variant, err := library.NewTrack(userId, "Other Song", "  Beyoncé  ", "  I Am...   Sasha Fierce  ", time.Now)
	if err != nil {
		t.Fatalf("NewTrack(variant): %v", err)
	}

	if clean.Artist != variant.Artist {
		t.Errorf("artist not coalesced: clean %q vs variant %q", clean.Artist, variant.Artist)
	}
	if clean.Album != variant.Album {
		t.Errorf("album not coalesced: clean %q vs variant %q", clean.Album, variant.Album)
	}
	if clean.Album != "I Am... Sasha Fierce" {
		t.Errorf("album display mangled: %q", clean.Album)
	}
}

func TestSetAlbumArtist_Canonicalizes(t *testing.T) {
	t.Parallel()
	userId := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userId, "Song", "Artist", "Album", time.Now)
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
			if track.DownloadStatus != library.AcquisitionReady {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionReady", track.DownloadStatus)
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
			if track.DownloadStatus != library.AcquisitionFailed {
				t.Errorf("AcquisitionStatus = %v, want AcquisitionFailed", track.DownloadStatus)
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

	if err := track.RevertToPending(time.Now); err != nil {
		t.Fatalf("RevertToPending: %v", err)
	}

	if track.DownloadStatus != library.AcquisitionPending {
		t.Errorf("AcquisitionStatus = %v, want AcquisitionPending", track.DownloadStatus)
	}
	if track.AudioRef != nil {
		t.Errorf("AudioRef should be nil after RevertToPending, got %q", *track.AudioRef)
	}
	if track.FailureReason != nil {
		t.Errorf("FailureReason should be nil after RevertToPending, got %q", *track.FailureReason)
	}
}

func TestTrack_SetAcquisitionConfidence(t *testing.T) {
	tests := []struct {
		name       string
		confidence float64
		evidence   string
		wantErr    bool
	}{
		{"zero", 0, `{"verdict":"hard"}`, false},
		{"one", 1, `{"verdict":"hard"}`, false},
		{"middle without evidence", 0.5, "", false},
		{"above one", 1.01, `{}`, true},
		{"below zero", -0.01, `{}`, true},
		{"NaN", math.NaN(), `{}`, true},
		{"malformed evidence", 0.5, `{"verdict":`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track, err := library.NewTrack(ids.NewUserId(uuid.New()), "Song", "Artist", "Album", time.Now)
			if err != nil {
				t.Fatalf("NewTrack: %v", err)
			}
			err = track.SetAcquisitionConfidence(tt.confidence, json.RawMessage(tt.evidence))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if track.ConfidenceScore != nil || track.EvidenceJSON != nil {
					t.Errorf("rejected call mutated the track: %v %s", track.ConfidenceScore, track.EvidenceJSON)
				}
				return
			}
			if got := track.AcquisitionConfidence(); got != tt.confidence {
				t.Errorf("AcquisitionConfidence() = %v, want %v", got, tt.confidence)
			}
			if got := string(track.AcquisitionEvidence()); got != tt.evidence {
				t.Errorf("AcquisitionEvidence() = %q, want %q", got, tt.evidence)
			}
		})
	}
}

func TestTrack_IsStreamable(t *testing.T) {
	t.Parallel()
	audioRef := "s3://bucket/track.opus"

	tests := []struct {
		name  string
		setup func(*library.Track)
		want  bool
	}{
		{
			name: "ready with audioRef is streamable",
			setup: func(tr *library.Track) {
				tr.DownloadStatus = library.AcquisitionReady
				tr.AudioRef = &audioRef
			},
			want: true,
		},
		{
			name: "ready without audioRef is not streamable",
			setup: func(tr *library.Track) {
				tr.DownloadStatus = library.AcquisitionReady
				tr.AudioRef = nil
			},
			want: false,
		},
		{
			name: "pending is not streamable",
			setup: func(tr *library.Track) {
				tr.DownloadStatus = library.AcquisitionPending
			},
			want: false,
		},
		{
			name: "failed is not streamable",
			setup: func(tr *library.Track) {
				tr.DownloadStatus = library.AcquisitionFailed
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

func newTestTrack(t *testing.T) *library.Track {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userId, "Test Title", "Test Artist", "Test Album", time.Now)
	if err != nil {
		t.Fatalf("newTestTrack: unexpected error: %v", err)
	}
	return track
}

func TestSetAlbum_KeepsTheDedupKeyInStep(t *testing.T) {
	t.Parallel()
	userId := ids.NewUserId(uuid.New())
	track, err := library.NewTrack(userId, "Crook", "Raf Saperra", "Some Album", time.Now)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	track.SetAlbum("Other Album")

	if track.Album != "Other Album" {
		t.Errorf("Album = %q, want %q", track.Album, "Other Album")
	}
	if want := library.TrackDedupKey("Crook", "Raf Saperra", "Other Album"); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
}

func TestSetAlbum_BlankFallsBackToTheTitle(t *testing.T) {
	t.Parallel()
	track := &library.Track{Title: "Crook", Artist: "Raf Saperra", Album: ""}

	track.SetAlbum("")

	if track.Album != "Crook" {
		t.Errorf("Album = %q, want the title", track.Album)
	}
	if want := library.TrackDedupKey("Crook", "Raf Saperra", "Crook"); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
}

func TestTrackTextLengthMessages(t *testing.T) {
	t.Parallel()
	overlong := strings.Repeat("x", 301)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"optional album", library.ValidateOptionalTrackText(&overlong, "album"), "track album exceeds 300 characters"},
		{"optional genre", library.ValidateOptionalTrackText(&overlong, "genre"), "track genre exceeds 300 characters"},
		{"source url", library.ValidateSourceURL("https://example.com/" + overlong), "track source_url exceeds 300 characters"},
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

func TestNulByteRefusedByEveryTextEntryPoint(t *testing.T) {
	t.Parallel()
	userId := ids.NewUserId(uuid.New())
	newTrackErr := func(title, artist, album string) error {
		_, err := library.NewTrack(userId, title, artist, album, time.Now)
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
			func(s string) error { return library.ValidateOptionalTrackText(&s, "genre") },
			"track genre must not contain a NUL byte",
		},
		{
			"playlist name",
			func(s string) error {
				_, err := library.NewPlaylist(userId, s, time.Unix(0, 0))
				return err
			},
			"playlist name must not contain a NUL byte",
		},
		{
			"featured artist name",
			func(s string) error { return library.ValidateFeaturedArtist(library.FeaturedArtist{Name: s}) },
			"track featured_artists name must not contain a NUL byte",
		},
		{
			"featured artist mbid",
			func(s string) error { return library.ValidateFeaturedArtist(library.FeaturedArtist{MBID: s}) },
			"track featured_artists mbid must not contain a NUL byte",
		},
		{
			"source url",
			func(s string) error { return library.ValidateSourceURL("https://example.com/" + s) },
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
			testdb.AssertValidationError(t, err)
			if err := tt.validate("ab"); err != nil {
				t.Errorf("same text without the NUL was rejected: %v", err)
			}
		})
	}
}

func markerUserId(t *testing.T) ids.UserId {
	t.Helper()
	return ids.NewUserId(uuid.New())
}

func TestNewTrack_SetsInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := library.NewTrack(markerUserId(t), "Title", "Artist", "Album", time.Now)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if track.DownloadStatus != library.AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.DownloadStatus)
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
	track, err := library.NewTrack(markerUserId(t), "Title", "Artist", "Album", time.Now)
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
	track, err := library.NewTrack(markerUserId(t), "Title", "Artist", "Album", time.Now)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed(string(library.FailureAcquisitionInterrupted)); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if track.AcquisitionStartedAt != nil {
		t.Errorf("AcquisitionStartedAt = %v, want nil after MarkFailed", track.AcquisitionStartedAt)
	}
}

func TestTrack_RevertToPending_RefreshesInFlightMarker(t *testing.T) {
	t.Parallel()
	track, err := library.NewTrack(markerUserId(t), "Title", "Artist", "Album", time.Now)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed("download_failed"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if err := track.RevertToPending(time.Now); err != nil {
		t.Fatalf("RevertToPending: %v", err)
	}

	if track.DownloadStatus != library.AcquisitionPending {
		t.Fatalf("status = %v, want pending", track.DownloadStatus)
	}
	if track.AcquisitionStartedAt == nil {
		t.Fatal("AcquisitionStartedAt = nil, want a fresh marker after RevertToPending")
	}
}

func TestFailureMessage_AcquisitionInterrupted(t *testing.T) {
	t.Parallel()
	reason := string(library.FailureAcquisitionInterrupted)
	if got := library.FailureMessage(&reason); got != "Acquisition was interrupted" {
		t.Errorf("FailureMessage = %q, want %q", got, "Acquisition was interrupted")
	}
}

func TestAcquisitionFailureCodes_KeepTheirStoredValues(t *testing.T) {
	t.Parallel()
	stored := []struct {
		code library.FailureCode
		want string
	}{
		{library.FailureAcquisitionInterrupted, "acquisition_interrupted"},
		{library.FailureAcquisitionRefused, "acquisition_refused"},
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
		"above cap": maxDurationSeconds + 1,
		"max float": math.MaxFloat64,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			track := &library.Track{}

			err := track.SetDuration(seconds)

			testdb.AssertValidationError(t, err)
			if track.DurationSeconds != nil {
				t.Errorf("DurationSeconds = %v, want unset", *track.DurationSeconds)
			}
		})
	}
}

func TestTrackSetDuration_StoresPositiveAndIgnoresZero(t *testing.T) {
	t.Parallel()
	track := &library.Track{}

	if err := track.SetDuration(0); err != nil || track.DurationSeconds != nil {
		t.Fatalf("SetDuration(0) = %v, duration %v; want nil error and unset", err, track.DurationSeconds)
	}
	if err := track.SetDuration(maxDurationSeconds); err != nil {
		t.Fatalf("SetDuration(max) error = %v", err)
	}
	if track.DurationSeconds == nil || *track.DurationSeconds != maxDurationSeconds {
		t.Errorf("DurationSeconds = %v, want %d", track.DurationSeconds, maxDurationSeconds)
	}
}

func TestTotalDurationSeconds_FiniteAtCap(t *testing.T) {
	t.Parallel()
	tracks := make([]*library.Track, 10000)
	for i := range tracks {
		tracks[i] = &library.Track{}
		if err := tracks[i].SetDuration(maxDurationSeconds); err != nil {
			t.Fatal(err)
		}
	}

	if total := library.TotalDurationSeconds(tracks); math.IsInf(total, 0) || math.IsNaN(total) {
		t.Errorf("TotalDurationSeconds = %v, want finite", total)
	}
}

const transitionRef = "s3://bucket/track.opus"

type trackState func(t *testing.T) *library.Track

func pendingTrack(t *testing.T) *library.Track {
	t.Helper()
	return newTestTrack(t)
}

func readyTrack(t *testing.T) *library.Track {
	t.Helper()
	track := newTestTrack(t)
	if err := track.MarkReady(transitionRef); err != nil {
		t.Fatalf("setup MarkReady: %v", err)
	}
	return track
}

func failedTrack(t *testing.T) *library.Track {
	t.Helper()
	track := newTestTrack(t)
	if err := track.MarkFailed("download_failed"); err != nil {
		t.Fatalf("setup MarkFailed: %v", err)
	}
	return track
}

func readyWithoutAudioTrack(t *testing.T) *library.Track {
	t.Helper()
	track := newTestTrack(t)
	track.DownloadStatus = library.AcquisitionReady
	track.AcquisitionStartedAt = nil
	return track
}

var transitions = map[string]func(*library.Track) error{
	"MarkReady":        func(tr *library.Track) error { return tr.MarkReady("s3://bucket/other.opus") },
	"MarkReadySameRef": func(tr *library.Track) error { return tr.MarkReady(transitionRef) },
	"ReplaceAudio":     func(tr *library.Track) error { return tr.ReplaceAudio("s3://bucket/other.opus") },
	"MarkFailed":       func(tr *library.Track) error { return tr.MarkFailed("audio file missing from storage") },
	"FailAcquisition":  func(tr *library.Track) error { return tr.FailAcquisition("no_match_found") },
	"RevertToPending":  func(tr *library.Track) error { return tr.RevertToPending(time.Now) },
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

			if !errors.Is(err, library.ErrIllegalAcquisitionTransition) {
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
		want   library.DownloadStatus
	}{
		{"MarkReady from pending (acquisition completed)", pendingTrack, "MarkReady", library.AcquisitionReady},
		{"MarkReady from failed (late success, backfill)", failedTrack, "MarkReady", library.AcquisitionReady},
		{"MarkReady on ready without audio (backfill repair)", readyWithoutAudioTrack, "MarkReady", library.AcquisitionReady},
		{"MarkReady on ready under the same ref (duplicate completion rewrote it)", readyTrack, "MarkReadySameRef", library.AcquisitionReady},
		{"ReplaceAudio on a ready track", readyTrack, "ReplaceAudio", library.AcquisitionReady},
		{"MarkFailed from pending (refused schedule, stale sweep)", pendingTrack, "MarkFailed", library.AcquisitionFailed},
		{"MarkFailed from ready (audio missing from storage)", readyTrack, "MarkFailed", library.AcquisitionFailed},
		{"FailAcquisition from pending", pendingTrack, "FailAcquisition", library.AcquisitionFailed},
		{"RevertToPending from ready (re-acquire)", readyTrack, "RevertToPending", library.AcquisitionPending},
		{"RevertToPending from failed (retry)", failedTrack, "RevertToPending", library.AcquisitionPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			track := tc.from(t)

			if err := transitions[tc.method](track); err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if track.DownloadStatus != tc.want {
				t.Errorf("status = %v, want %v", track.DownloadStatus, tc.want)
			}
		})
	}
}

func TestSetAcquisitionProvenance_RejectsUnknownValues(t *testing.T) {
	track := &library.Track{}

	track.SetAcquisitionProvenance("nonsense")
	if track.AcquisitionProvenance != nil {
		t.Errorf("an unknown provenance must not be written, got %q", *track.AcquisitionProvenance)
	}

	track.SetAcquisitionProvenance(library.ProvenanceVerified)
	if track.AcquisitionProvenance == nil || *track.AcquisitionProvenance != "verified" {
		t.Errorf("provenance = %v, want verified", track.AcquisitionProvenance)
	}
}

const reRecordingMBID = "25d68187-ee71-4f06-9e58-0b49d66bf8a8"

func studioLabelledTrack(t *testing.T) *library.Track {
	t.Helper()
	track, err := library.NewTrack(ids.NewUserId(uuid.New()), "It's Only Love", "Bryan Adams", "Reckless", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	studioMBID, studioISRC, studioYear, studioLength := "7933a65c-2c66-4ec1-9880-e46b5d2f05de", "USAM18490008", 1984, 195.0
	track.CatalogMBID, track.ISRC, track.Year, track.CatalogDurationSeconds = &studioMBID, &studioISRC, &studioYear, &studioLength
	return track
}

func TestTrack_RelabelAsRecordingNamesTheStoredRecording(t *testing.T) {
	t.Parallel()
	track := studioLabelledTrack(t)
	if track.Relabelled() {
		t.Fatal("Relabelled() = true before any relabel")
	}

	err := track.RelabelAsRecording(reRecordingMBID, "It’s Only Love (classic version)", "It’s Only Love", 2025, "IETLH2500023", 194)
	if err != nil {
		t.Fatal(err)
	}

	if track.Title != "It’s Only Love (classic version)" || track.Album != "It’s Only Love" {
		t.Errorf("title/album = %q / %q", track.Title, track.Album)
	}
	if track.Year == nil || *track.Year != 2025 {
		t.Errorf("Year = %v, want 2025", track.Year)
	}
	if track.ISRC == nil || *track.ISRC != "IETLH2500023" {
		t.Errorf("ISRC = %v, want IETLH2500023", track.ISRC)
	}
	if track.CatalogMBID == nil || *track.CatalogMBID != reRecordingMBID {
		t.Errorf("CatalogMBID = %v, want %s", track.CatalogMBID, reRecordingMBID)
	}
	if track.CatalogDurationSeconds == nil || *track.CatalogDurationSeconds != 194 {
		t.Errorf("CatalogDurationSeconds = %v, want 194", track.CatalogDurationSeconds)
	}
	if want := library.TrackDedupKey(track.Title, "Bryan Adams", track.Album); track.DedupKey != want {
		t.Errorf("DedupKey = %q, want %q", track.DedupKey, want)
	}
	if !track.Relabelled() {
		t.Error("Relabelled() = false after a relabel")
	}
}

func TestTrack_RelabelAsRecordingClearsWhatTheRecordingDoesNotKnow(t *testing.T) {
	t.Parallel()
	track := studioLabelledTrack(t)

	if err := track.RelabelAsRecording(reRecordingMBID, "Live Cut", "", 0, "", 0); err != nil {
		t.Fatal(err)
	}

	if track.Album != "Live Cut" {
		t.Errorf("Album = %q, want the title", track.Album)
	}
	if track.Year != nil || track.ISRC != nil || track.CatalogDurationSeconds != nil {
		t.Errorf("year/isrc/length = %v/%v/%v, want all cleared", track.Year, track.ISRC, track.CatalogDurationSeconds)
	}
}

func TestTrack_RelabelAsRecordingRefusesAndChangesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, mbid, title string
		duration          float64
	}{
		{name: "empty mbid", mbid: "", title: "Title"},
		{name: "malformed mbid", mbid: "nope", title: "Title"},
		{name: "empty title", mbid: reRecordingMBID, title: ""},
		{name: "title with NUL", mbid: reRecordingMBID, title: "a\x00b"},
		{name: "negative length", mbid: reRecordingMBID, title: "Title", duration: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := studioLabelledTrack(t)
			before := *track

			if err := track.RelabelAsRecording(tt.mbid, tt.title, "Album", 2025, "IETLH2500023", tt.duration); err == nil {
				t.Fatal("expected an error")
			}

			if !reflect.DeepEqual(*track, before) {
				t.Errorf("track changed on a refused relabel: %+v", *track)
			}
			if track.Relabelled() {
				t.Error("Relabelled() = true after a refused relabel")
			}
		})
	}
}

func TestTrack_ARelabelledTrackKeepsItsArtistAndDedupsLikeOneAddedUnderTheNewLabels(t *testing.T) {
	t.Parallel()
	track := studioLabelledTrack(t)
	studioKey := track.DedupKey

	if err := track.RelabelAsRecording(reRecordingMBID, "It’s Only Love (classic version)", "It’s Only Love", 2025, "IETLH2500023", 194); err != nil {
		t.Fatal(err)
	}

	if track.Artist != "Bryan Adams" {
		t.Errorf("Artist = %q, want the artist kept", track.Artist)
	}
	if track.DedupKey == studioKey {
		t.Error("DedupKey still names the studio recording after a relabel")
	}
	addedFresh, err := library.NewTrack(track.UserId, "It’s Only Love (classic version)", "Bryan Adams", "It’s Only Love", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if track.DedupKey != addedFresh.DedupKey {
		t.Errorf("DedupKey = %q, want %q (the key a track added under the new labels gets)", track.DedupKey, addedFresh.DedupKey)
	}
}

func TestTrack_RelabelAsRecordingRefusesBadAlbumTextAndChangesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, album, isrc string }{
		{name: "album with NUL", album: "a\x00b", isrc: "IETLH2500023"},
		{name: "album past the text limit", album: strings.Repeat("a", library.MaxLibrarySearchLength+1), isrc: "IETLH2500023"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := studioLabelledTrack(t)
			before := *track

			if err := track.RelabelAsRecording(reRecordingMBID, "Title", tt.album, 2025, tt.isrc, 194); err == nil {
				t.Fatal("expected an error")
			}
			if !reflect.DeepEqual(*track, before) {
				t.Errorf("track changed on a refused relabel: %+v", *track)
			}
			if track.Relabelled() {
				t.Error("Relabelled() = true after a refused relabel")
			}
		})
	}
}

func TestTrack_RelabelAsRecordingAcceptsAnAlbumAtTheTextLimit(t *testing.T) {
	t.Parallel()
	track := studioLabelledTrack(t)
	album := strings.Repeat("a", library.MaxLibrarySearchLength)

	if err := track.RelabelAsRecording(reRecordingMBID, "Title", album, 2025, "IETLH2500023", 194); err != nil {
		t.Fatalf("album at the limit refused: %v", err)
	}
	if track.Album != album {
		t.Error("album at the limit not stored")
	}
}

func TestTrack_RejectAudioSourceIsIdempotentAndBounded(t *testing.T) {
	t.Parallel()
	track := &library.Track{}

	track.RejectAudioSource("youtube:aaa")
	track.RejectAudioSource("youtube:aaa")
	if len(track.RejectedSourceKeys) != 1 {
		t.Errorf("keys = %v, want no duplicate", track.RejectedSourceKeys)
	}

	track.RejectAudioSource("")
	if len(track.RejectedSourceKeys) != 1 {
		t.Errorf("keys = %v, want an empty key ignored", track.RejectedSourceKeys)
	}

	var added []string
	for i := range 60 {
		key := string(rune('a'+i%26)) + string(rune('0'+i/26))
		added = append(added, key)
		track.RejectAudioSource(key)
	}
	if len(track.RejectedSourceKeys) > 25 {
		t.Errorf("keys = %d, want the list bounded so a row cannot grow without limit", len(track.RejectedSourceKeys))
	}
	if want := added[len(added)-25:]; !reflect.DeepEqual(track.RejectedSourceKeys, want) {
		t.Errorf("keys = %v, want the 25 most recent rejections %v", track.RejectedSourceKeys, want)
	}
}

func audioLength(t *testing.T, seconds float64) tracklength.TrackLength {
	t.Helper()
	length, err := tracklength.TrackLengthOf(seconds)
	if err != nil {
		t.Fatal(err)
	}
	return length
}

func TestTrack_RecordAudioLengthFlagsAudioThatIsNotTheCatalogRecording(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		catalog  float64
		audio    float64
		wantFlag bool
	}{
		{"five seconds off a short track", 100, 105, false},
		{"just past five seconds off a short track", 100, 105.1, true},
		{"three percent off a long track", 400, 388, false},
		{"just past three percent off a long track", 400, 387.9, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			track := &library.Track{AudioLengthMismatch: !tt.wantFlag}
			if err := track.SetCatalogDuration(tt.catalog); err != nil {
				t.Fatal(err)
			}

			if !track.RecordAudioLength(audioLength(t, tt.audio)) {
				t.Fatalf("RecordAudioLength(%v s) = false, want the audio checked against the %v s catalog length", tt.audio, tt.catalog)
			}
			if track.AudioLengthMismatch != tt.wantFlag {
				t.Errorf("AudioLengthMismatch = %v, want %v", track.AudioLengthMismatch, tt.wantFlag)
			}
		})
	}
}

func TestTrack_RecordAudioLengthLeavesTheFlagWhileEitherLengthIsUnknown(t *testing.T) {
	t.Parallel()
	known, unknown, implausible := 200.0, 0.0, -1.0
	tests := []struct {
		name    string
		catalog *float64
		audio   float64
	}{
		{"no catalog length", nil, 200},
		{"a stored catalog length of zero", &unknown, 200},
		{"a stored catalog length no recording can have", &implausible, 200},
		{"no measured length", &known, 0},
	}
	for _, tt := range tests {
		for name, flagged := range map[string]bool{"flagged": true, "unflagged": false} {
			t.Run(tt.name+", "+name, func(t *testing.T) {
				t.Parallel()
				track := &library.Track{CatalogDurationSeconds: tt.catalog, AudioLengthMismatch: flagged}

				if track.RecordAudioLength(audioLength(t, tt.audio)) {
					t.Error("RecordAudioLength = true, want nothing compared while a length is unknown")
				}
				if track.AudioLengthMismatch != flagged {
					t.Errorf("AudioLengthMismatch = %v, want it left at %v", track.AudioLengthMismatch, flagged)
				}
			})
		}
	}
}

func TestNewTrack_RefusesANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewTrack accepted a nil clock, want a panic")
		}
	}()

	_, _ = library.NewTrack(ids.NewUserId(uuid.New()), "Title", "Artist", "Album", nil)
}

func TestNewTrack_StampsAddedAtFromTheInjectedClock(t *testing.T) {
	pinned := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)

	track, err := library.NewTrack(ids.NewUserId(uuid.New()), "Title", "Artist", "Album", func() time.Time { return pinned })
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if !track.AddedAt.Equal(pinned) {
		t.Errorf("AddedAt = %v, want the injected %v", track.AddedAt, pinned)
	}
}

func TestRevertToPending_StampsTheStartFromTheInjectedClock(t *testing.T) {
	pinned := time.Date(2026, 11, 26, 11, 0, 0, 0, time.UTC)
	track, err := library.NewTrack(ids.NewUserId(uuid.New()), "Title", "Artist", "Album", time.Now)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if err := track.MarkFailed("boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	if err := track.RevertToPending(func() time.Time { return pinned }); err != nil {
		t.Fatalf("RevertToPending: %v", err)
	}
	if track.AcquisitionStartedAt == nil || !track.AcquisitionStartedAt.Equal(pinned) {
		t.Errorf("AcquisitionStartedAt = %v, want the injected %v", track.AcquisitionStartedAt, pinned)
	}
}
