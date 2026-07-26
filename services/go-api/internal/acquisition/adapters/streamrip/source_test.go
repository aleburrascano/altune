package streamrip

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

func identityWith(provider, externalID, url string) ports.RecordingIdentity {
	return ports.RecordingIdentity{
		Duration: 200,
		Sources:  []ports.RecordingSource{{Provider: provider, ExternalID: externalID, URL: url}},
	}
}

func TestFind_BuildsTrackURLPerService(t *testing.T) {
	tests := []struct {
		service string
		id      string
		want    string
	}{
		{"tidal", "103805726", "https://tidal.com/browse/track/103805726"},
		{"deezer", "3135556", "https://www.deezer.com/track/3135556"},
		{"qobuz", "12345", "https://open.qobuz.com/track/12345"},
	}

	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			src := NewSource(tt.service)
			got, err := src.Find(context.Background(), ports.FindRequest{
				Title:    "Song",
				Identity: identityWith(tt.service, tt.id, ""),
			})
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("candidates = %d, want 1", len(got))
			}
			if got[0].URL != tt.want {
				t.Errorf("URL = %q, want %q", got[0].URL, tt.want)
			}
			if !got[0].Resolved {
				t.Error("a catalog-resolved candidate must be marked Resolved")
			}
		})
	}
}

func TestFind_SoundCloudUsesThePermalinkNotAnID(t *testing.T) {
	src := NewSource("soundcloud")

	got, err := src.Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("soundcloud", "999", "https://soundcloud.com/artist/song"),
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://soundcloud.com/artist/song" {
		t.Fatalf("candidates = %+v, want the permalink", got)
	}
}

func TestFind_SoundCloudWithoutPermalinkYieldsNothing(t *testing.T) {
	src := NewSource("soundcloud")

	got, _ := src.Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("soundcloud", "999", ""),
	})
	if len(got) != 0 {
		t.Errorf("a numeric id is not a downloadable SoundCloud URL; want none, got %+v", got)
	}
}

func TestFind_NoMatchingProviderYieldsNothing(t *testing.T) {
	src := NewSource("tidal")

	got, err := src.Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("deezer", "123", ""),
	})
	if err != nil {
		t.Fatalf("an unresolvable track is not an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %d, want none", len(got))
	}
}

func TestName_NamespacesByService(t *testing.T) {
	if got := NewSource("tidal").Name(); got != "streamrip:tidal" {
		t.Errorf("Name() = %q", got)
	}
	if got := NewSource("deezer").Name(); got != "streamrip:deezer" {
		t.Errorf("Name() = %q", got)
	}
}

func TestSupported(t *testing.T) {
	for _, s := range []string{"tidal", "deezer", "qobuz", "soundcloud"} {
		if !Supported(s) {
			t.Errorf("Supported(%q) = false", s)
		}
	}
	if Supported("spotify") {
		t.Error("Supported(spotify) = true; streamrip cannot fetch Spotify audio")
	}
}

func TestLargestAudioFile_PicksBiggestAndIgnoresNonAudio(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "Artist", "Album")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, size int) string {
		path := filepath.Join(nested, name)
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("cover.jpg", 500_000)
	write("small.mp3", 20_000)
	want := write("full.flac", 900_000)

	got, err := largestAudioFile(dir)
	if err != nil {
		t.Fatalf("largestAudioFile: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLargestAudioFile_RejectsTinyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stub.mp3"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := largestAudioFile(dir); err == nil {
		t.Error("expected a too-small file to be rejected")
	}
}

func TestLargestAudioFile_NoAudioIsAnError(t *testing.T) {
	if _, err := largestAudioFile(t.TempDir()); err == nil {
		t.Error("expected an error when streamrip produced no audio")
	}
}

func TestWithBinary_IgnoresEmpty(t *testing.T) {
	src := NewSource("tidal").WithBinary("")
	if src.bin != defaultBin {
		t.Errorf("bin = %q, want the default %q", src.bin, defaultBin)
	}
	if got := NewSource("tidal").WithBinary("/usr/bin/rip").bin; got != "/usr/bin/rip" {
		t.Errorf("bin = %q", got)
	}
}
