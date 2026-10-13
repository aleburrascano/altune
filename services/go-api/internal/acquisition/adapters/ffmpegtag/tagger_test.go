package ffmpegtag

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeFfmpeg(t *testing.T, body string) (binary, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "ffmpeg")
	argvFile = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\nfor last; do :; done\n" + body + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, argvFile
}

func audioFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type recordingTagger struct{ paths []string }

func (r *recordingTagger) Tag(_ context.Context, filePath string, _ ports.TrackTags) error {
	r.paths = append(r.paths, filePath)
	return nil
}

var fullTags = ports.TrackTags{
	Title: "Remember Them Days", Artist: "Slayr", Album: "Remember Them Days",
	AlbumArtist: "Slayr", Year: 2024, TrackNumber: 3, Genre: "Hip-Hop",
}

func TestTagger_WritesTagsInACopyPassAndReplacesTheFile(t *testing.T) {
	binary, argvFile := fakeFfmpeg(t, `printf tagged > "$last"`)
	path := audioFile(t, "audio.m4a", "original")

	if err := NewTagger(binary, &recordingTagger{}).Tag(context.Background(), path, fullTags); err != nil {
		t.Fatalf("Tag() = %v", err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(raw)
	for _, want := range []string{
		"-c\ncopy\n", "-movflags\n+faststart\n",
		"-metadata\ntitle=Remember Them Days\n", "-metadata\nartist=Slayr\n",
		"-metadata\nalbum=Remember Them Days\n", "-metadata\nalbum_artist=Slayr\n",
		"-metadata\ndate=2024\n", "-metadata\ntrack=3\n", "-metadata\ngenre=Hip-Hop\n",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("ffmpeg argv = %q, want it to contain %q", argv, want)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != "tagged" {
		t.Errorf("file = %q, want the tagged output in place of the original", got)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*")); len(leftovers) != 1 {
		t.Errorf("dir holds %q, want only the tagged file", leftovers)
	}
}

func TestTagger_LeavesOptionalTagsOutWhenUnknown(t *testing.T) {
	binary, argvFile := fakeFfmpeg(t, `printf tagged > "$last"`)
	path := audioFile(t, "audio.m4a", "original")

	tags := ports.TrackTags{Title: "Gorah", Artist: "Nitefreak"}
	if err := NewTagger(binary, &recordingTagger{}).Tag(context.Background(), path, tags); err != nil {
		t.Fatalf("Tag() = %v", err)
	}
	raw, _ := os.ReadFile(argvFile)
	for _, absent := range []string{"album=", "album_artist=", "date=", "track=", "genre="} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("ffmpeg argv = %q, want no %q for an unknown value", raw, absent)
		}
	}
}

func TestTagger_KeepsTheOriginalWhenFfmpegFails(t *testing.T) {
	binary, _ := fakeFfmpeg(t, `printf partial > "$last"; echo 'moov atom not found' >&2; exit 1`)
	path := audioFile(t, "audio.m4a", "original")

	err := NewTagger(binary, &recordingTagger{}).Tag(context.Background(), path, fullTags)
	if err == nil || !strings.Contains(err.Error(), "moov atom not found") {
		t.Fatalf("Tag() = %v, want ffmpeg's error", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "original" {
		t.Errorf("file = %q, want the original untouched", got)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*")); len(leftovers) != 1 {
		t.Errorf("dir holds %q, want no partial output left behind", leftovers)
	}
}

func TestTagger_HandsNonM4AFilesToTheFallback(t *testing.T) {
	binary, argvFile := fakeFfmpeg(t, "exit 1")
	path := audioFile(t, "audio.mp3", "mp3 bytes")
	fallback := &recordingTagger{}

	if err := NewTagger(binary, fallback).Tag(context.Background(), path, fullTags); err != nil {
		t.Fatalf("Tag() = %v", err)
	}
	if len(fallback.paths) != 1 || fallback.paths[0] != path {
		t.Errorf("fallback got %q, want %q", fallback.paths, path)
	}
	if _, err := os.Stat(argvFile); err == nil {
		t.Error("ffmpeg ran for an mp3, want only the fallback")
	}
}
