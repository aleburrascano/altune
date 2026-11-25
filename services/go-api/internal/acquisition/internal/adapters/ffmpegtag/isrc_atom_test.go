package ffmpegtag_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/ffmpegtag"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mp4Box(kind string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out, uint32(8+len(payload)))
	copy(out[4:], kind)
	return append(out, payload...)
}

func moovWithAnEmptyTagList() []byte {
	ilst := mp4Box("ilst", nil)
	meta := mp4Box("meta", append(make([]byte, 4), ilst...))
	return mp4Box("moov", mp4Box("udta", meta))
}

func ffmpegProducing(t *testing.T, fixture []byte, afterwards string) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "tagged.m4a")
	if err := os.WriteFile(source, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	ffmpeg, _ := fakeFfmpeg(t, `cp "`+source+`" "$last"`+afterwards)
	return ffmpeg
}

func TestTagger_WithAnISRCWritesTheFreeformAtomIntoTheTagList(t *testing.T) {
	fixture := moovWithAnEmptyTagList()
	path := audioFile(t, "audio.m4a", "original")
	tags := fullTags
	tags.ISRC = "USRC17607839"

	if err := ffmpegtag.NewTagger(ffmpegProducing(t, fixture, ""), nil).Tag(context.Background(), path, tags); err != nil {
		t.Fatalf("Tag() = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("com.apple.iTunes")) || !bytes.Contains(raw, []byte(tags.ISRC)) {
		t.Errorf("tagged file %q holds no ISRC freeform atom", raw)
	}
	if len(raw) <= len(fixture) {
		t.Errorf("tagged file is %d bytes, want it larger than the %d-byte tag list it started from", len(raw), len(fixture))
	}
	if got := int(binary.BigEndian.Uint32(raw)); got != len(raw) {
		t.Errorf("moov box says %d bytes, file is %d: the enclosing boxes were not grown by the atom", got, len(raw))
	}
}

func TestTagger_AnISRCTheFileCannotTakeKeepsTheOriginalAndReportsIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not stop root")
	}
	path := audioFile(t, "audio.m4a", "original")
	tags := fullTags
	tags.ISRC = "USRC17607839"
	ffmpeg := ffmpegProducing(t, moovWithAnEmptyTagList(), "\nchmod 0400 \"$last\"")

	err := ffmpegtag.NewTagger(ffmpeg, nil).Tag(context.Background(), path, tags)

	if err == nil || !strings.Contains(err.Error(), "embed isrc") {
		t.Errorf("Tag() = %v, want an embed isrc error", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "original" {
		t.Errorf("file = %q, want the original kept", raw)
	}
}
