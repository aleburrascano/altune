package ffmpegtag

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
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

func TestTagger_EmbedsTheISRCWithoutMovingTheStandardTags(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.m4a")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=duration=1", "-c:a", "aac", source).CombinedOutput(); err != nil {
		t.Fatalf("make fixture: %v %s", err, out)
	}
	probe := func(path string) map[string]string {
		out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format_tags", "-of", "json", path).Output()
		if err != nil {
			t.Fatalf("ffprobe %s: %v", path, err)
		}
		var parsed struct {
			Format struct {
				Tags map[string]string `json:"tags"`
			} `json:"format"`
		}
		if err := json.Unmarshal(out, &parsed); err != nil {
			t.Fatal(err)
		}
		tags := map[string]string{}
		for k, v := range parsed.Format.Tags {
			tags[strings.ToLower(k)] = v
		}
		return tags
	}
	decodes := func(path string) {
		if out, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-f", "null", "-").CombinedOutput(); err != nil || len(out) != 0 {
			t.Fatalf("decode %s: %v %s", path, err, out)
		}
	}

	withISRC := filepath.Join(dir, "with.m4a")
	without := filepath.Join(dir, "without.m4a")
	for _, p := range []string{withISRC, without} {
		raw, _ := os.ReadFile(source)
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tagger := NewTagger(ffmpeg, &recordingTagger{})
	tags := fullTags
	if err := tagger.Tag(context.Background(), without, tags); err != nil {
		t.Fatalf("Tag() = %v", err)
	}
	tags.ISRC = "USRC17607839"
	if err := tagger.Tag(context.Background(), withISRC, tags); err != nil {
		t.Fatalf("Tag() = %v", err)
	}

	got := probe(withISRC)
	if got["isrc"] != "USRC17607839" {
		t.Errorf("isrc = %q, want USRC17607839", got["isrc"])
	}
	if got["title"] != fullTags.Title || got["artist"] != fullTags.Artist {
		t.Errorf("title/artist = %q/%q, want the standard tags kept", got["title"], got["artist"])
	}
	raw, _ := os.ReadFile(withISRC)
	if !bytes.Contains(raw, []byte("\xa9nam")) {
		t.Error("file has no ©nam atom, want the standard title atom kept")
	}
	if _, ok := probe(without)["isrc"]; ok {
		t.Error("an empty ISRC wrote an isrc tag, want none")
	}
	decodes(withISRC)
	decodes(without)
}

func probeTools(t *testing.T) (ffmpeg, ffprobe string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err = exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	return ffmpeg, ffprobe
}

func probeRun(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %q: %v %s", name, args, err, out)
	}
	return out
}

func probeFormatTags(t *testing.T, ffprobe, path string) map[string]string {
	t.Helper()
	out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format_tags", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var parsed struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{}
	for k, v := range parsed.Format.Tags {
		tags[strings.ToLower(k)] = v
	}
	return tags
}

func probeAudioMD5(t *testing.T, ffmpeg, path string) string {
	t.Helper()
	return strings.TrimSpace(string(probeRun(t, ffmpeg, "-v", "error", "-i", path, "-map", "0:a", "-f", "md5", "-")))
}

func probeDecodesCleanly(t *testing.T, ffmpeg, path string) {
	t.Helper()
	out, err := exec.Command(ffmpeg, "-v", "error", "-i", path, "-f", "null", "-").CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("decode %s: %v %s", path, err, out)
	}
}

func probeFixture(t *testing.T, ffmpeg, dir, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	full := append([]string{"-v", "error", "-y"}, args...)
	probeRun(t, ffmpeg, append(full, path)...)
	return path
}

func TestTagger_EmbeddingTheISRCKeepsEveryM4AShapePlayableAndTheAudioIdentical(t *testing.T) {
	ffmpeg, ffprobe := probeTools(t)
	dir := t.TempDir()
	cover := probeFixture(t, ffmpeg, dir, "cover.png", "-f", "lavfi", "-i", "color=c=red:s=64x64", "-frames:v", "1")
	sine := []string{"-f", "lavfi", "-i", "sine=frequency=440:duration=3"}
	cases := map[string][]string{
		"moov after mdat":            append(append([]string{}, sine...), "-c:a", "aac"),
		"moov before mdat":           append(append([]string{}, sine...), "-c:a", "aac", "-movflags", "+faststart"),
		"no udta or meta":            append(append([]string{}, sine...), "-c:a", "aac", "-map_metadata", "-1", "-fflags", "+bitexact"),
		"existing ilst":              append(append([]string{}, sine...), "-c:a", "aac", "-metadata", "title=Old Title", "-metadata", "comment=keep", "-metadata", "artist=Old Artist"),
		"cover art as a second trak": append(append([]string{}, sine...), "-i", cover, "-map", "0:a", "-map", "1:v", "-c:a", "aac", "-c:v", "png", "-disposition:v", "attached_pic"),
		"long file with many chunks": {"-f", "lavfi", "-i", "sine=frequency=440:duration=600", "-c:a", "aac", "-b:a", "64k"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			path := probeFixture(t, ffmpeg, t.TempDir(), "audio.m4a", args...)
			before := probeAudioMD5(t, ffmpeg, path)
			tags := fullTags
			tags.ISRC = "USRC17607839"

			if err := NewTagger(ffmpeg, &recordingTagger{}).Tag(context.Background(), path, tags); err != nil {
				t.Fatalf("Tag() = %v", err)
			}

			probeDecodesCleanly(t, ffmpeg, path)
			if after := probeAudioMD5(t, ffmpeg, path); after != before {
				t.Errorf("decoded audio %s, want %s as before tagging", after, before)
			}
			got := probeFormatTags(t, ffprobe, path)
			if got["isrc"] != "USRC17607839" || got["title"] != "Remember Them Days" || got["artist"] != "Slayr" {
				t.Errorf("tags = %v, want isrc USRC17607839, title Remember Them Days, artist Slayr", got)
			}
		})
	}
}

func TestTagger_LeavesAFragmentedM4AEitherUntouchedOrPlayable(t *testing.T) {
	ffmpeg, ffprobe := probeTools(t)
	path := probeFixture(t, ffmpeg, t.TempDir(), "audio.m4a", "-f", "lavfi", "-i", "sine=duration=5", "-c:a", "aac", "-movflags", "frag_keyframe+empty_moov+default_base_moof", "-frag_duration", "500000")
	original, _ := os.ReadFile(path)
	before := probeAudioMD5(t, ffmpeg, path)
	tags := fullTags
	tags.ISRC = "USRC17607839"

	err := NewTagger(ffmpeg, &recordingTagger{}).Tag(context.Background(), path, tags)

	t.Logf("Tag() on a fragmented m4a = %v", err)
	if err != nil {
		if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
			t.Fatalf("Tag() = %v and the file changed, want the original untouched on refusal", err)
		}
		return
	}
	probeDecodesCleanly(t, ffmpeg, path)
	if after := probeAudioMD5(t, ffmpeg, path); after != before {
		t.Errorf("decoded audio %s, want %s as before tagging", after, before)
	}
	if got := probeFormatTags(t, ffprobe, path); got["isrc"] != "USRC17607839" || got["title"] != "Remember Them Days" {
		t.Errorf("tags = %v, want isrc and title read back", got)
	}
}

func TestTagger_RetaggingAFileHoldsOneISRCAtomWithTheLatestValue(t *testing.T) {
	ffmpeg, ffprobe := probeTools(t)
	path := probeFixture(t, ffmpeg, t.TempDir(), "audio.m4a", "-f", "lavfi", "-i", "sine=duration=3", "-c:a", "aac")
	before := probeAudioMD5(t, ffmpeg, path)
	tagger := NewTagger(ffmpeg, &recordingTagger{})
	tags := fullTags

	for _, isrc := range []string{"USRC17607839", "USRC17607839", "GBAYE0601498"} {
		tags.ISRC = isrc
		if err := tagger.Tag(context.Background(), path, tags); err != nil {
			t.Fatalf("Tag(%s) = %v", isrc, err)
		}
	}

	probeDecodesCleanly(t, ffmpeg, path)
	if after := probeAudioMD5(t, ffmpeg, path); after != before {
		t.Errorf("decoded audio %s, want %s as before tagging", after, before)
	}
	raw, _ := os.ReadFile(path)
	if n := bytes.Count(raw, []byte("com.apple.iTunes")); n != 1 {
		t.Errorf("file holds %d freeform mean atoms, want exactly 1", n)
	}
	if bytes.Contains(raw, []byte("USRC17607839")) {
		t.Error("file still holds the earlier ISRC, want only the latest")
	}
	if got := probeFormatTags(t, ffprobe, path); got["isrc"] != "GBAYE0601498" || got["title"] != "Remember Them Days" {
		t.Errorf("tags = %v, want isrc GBAYE0601498 and the title kept", got)
	}
}

func TestTagger_WithoutAnISRCWritesTheSameBytesAsATagPassWithoutOne(t *testing.T) {
	ffmpeg, _ := probeTools(t)
	dir := t.TempDir()
	source := probeFixture(t, ffmpeg, dir, "source.m4a", "-f", "lavfi", "-i", "sine=duration=3", "-c:a", "aac")
	tagger := NewTagger(ffmpeg, &recordingTagger{})
	var outputs [][]byte
	for _, name := range []string{"a.m4a", "b.m4a"} {
		raw, _ := os.ReadFile(source)
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := tagger.Tag(context.Background(), path, fullTags); err != nil {
			t.Fatalf("Tag() = %v", err)
		}
		out, _ := os.ReadFile(path)
		outputs = append(outputs, out)
	}
	plain := filepath.Join(dir, "plain.m4a")
	probeRun(t, ffmpeg, "-v", "error", "-y", "-i", source, "-c", "copy", "-movflags", "+faststart",
		"-metadata", "title=Remember Them Days", "-metadata", "artist=Slayr", "-metadata", "album=Remember Them Days",
		"-metadata", "album_artist=Slayr", "-metadata", "date=2024", "-metadata", "track=3", "-metadata", "genre=Hip-Hop", plain)
	want, _ := os.ReadFile(plain)
	if len(outputs) != 2 || outputs[0] == nil || outputs[1] == nil {
		t.Fatalf("got %d tagged outputs, want 2 readable files", len(outputs))
	}

	if !bytes.Equal(outputs[0], outputs[1]) {
		t.Error("two tag passes without an ISRC differ, want identical bytes")
	}
	if bytes.Contains(outputs[0], []byte("----")) || bytes.Contains(outputs[0], []byte("com.apple.iTunes")) {
		t.Error("file holds a freeform atom, want none without an ISRC")
	}
	if len(outputs[0]) != len(want) {
		t.Errorf("tagged size %d, want %d as a plain ffmpeg tag pass", len(outputs[0]), len(want))
	}
}
