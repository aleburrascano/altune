package main

import (
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func randomPrint(seed uint64, items int) []uint32 {
	source := rand.New(rand.NewPCG(seed, seed))
	items32 := make([]uint32, items)
	for i := range items32 {
		items32[i] = source.Uint32()
	}
	return items32
}

func withFlippedBits(original []uint32, every int) []uint32 {
	flipped := make([]uint32, len(original))
	for i, item := range original {
		flipped[i] = item
		if i%every == 0 {
			flipped[i] ^= 0b111
		}
	}
	return flipped
}

func recordedPrint(t *testing.T, name string) []uint32 {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var items []uint32
	for _, field := range strings.Split(strings.TrimSpace(string(raw)), ",") {
		item, err := strconv.ParseUint(field, 10, 32)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		items = append(items, uint32(item))
	}
	return items
}

func TestBestAlignment_FindsWhereThePreviewSitsInTheFile(t *testing.T) {
	file := randomPrint(1, 1400)
	tests := []struct {
		name       string
		preview    []uint32
		wantOffset int
		wantMaxBER float64
	}{
		{"exact copy at the start", file[:240], 0, 0},
		{"exact copy in the middle", file[300:540], 300, 0},
		{"exact copy flush with the end", file[1160:], 1160, 0},
		{"copy with 3 bits flipped in every 8th item", withFlippedBits(file[500:740], 8), 500, 3.0 / 256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bestAlignment(file, tt.preview)
			if err != nil {
				t.Fatalf("bestAlignment: %v", err)
			}
			if got.OffsetItems != tt.wantOffset || got.BitErrorRate > tt.wantMaxBER+1e-9 {
				t.Errorf("alignment = %+v, want offset %d with bit error rate <= %v", got, tt.wantOffset, tt.wantMaxBER)
			}
		})
	}
}

func TestBestAlignment_UnrelatedAudioScoresNearHalf(t *testing.T) {
	got, err := bestAlignment(randomPrint(2, 1400), randomPrint(3, 240))

	if err != nil || math.Abs(got.BitErrorRate-0.5) > 0.06 {
		t.Errorf("alignment = %+v, %v; want a bit error rate within 0.06 of 0.5", got, err)
	}
}

func TestBestAlignment_RefusesPrintsItCannotCompare(t *testing.T) {
	tests := []struct {
		name          string
		file, preview []uint32
		want          error
	}{
		{"empty file", nil, randomPrint(1, 10), errEmptyPrint},
		{"empty preview", randomPrint(1, 10), nil, errEmptyPrint},
		{"preview longer than the file", randomPrint(1, 10), randomPrint(2, 11), errPrintLongerThanRef},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := bestAlignment(tt.file, tt.preview); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBestAlignment_RecordedPairSeparatesTheSameSongFromAnotherByTheArtist(t *testing.T) {
	file := recordedPrint(t, "rich_flex_file_window.raw")

	same, err := bestAlignment(file, recordedPrint(t, "rich_flex_preview.raw"))
	if err != nil {
		t.Fatalf("same song: %v", err)
	}
	other, err := bestAlignment(file, recordedPrint(t, "chamber_of_reflection_preview.raw"))
	if err != nil {
		t.Fatalf("other song: %v", err)
	}

	if same.OffsetItems != 30 || same.BitErrorRate > 0.1 {
		t.Errorf("same song = %+v, want offset 30 with bit error rate <= 0.1", same)
	}
	if other.BitErrorRate < 0.35 {
		t.Errorf("other song = %+v, want bit error rate >= 0.35", other)
	}
}
