package deezerpreview

import (
	"errors"
	"fmt"
	"math/bits"
)

const (
	bitsPerItem         = 32
	itemDurationSeconds = 1365.0 / 11025.0
	MinPreviewItems     = 100
)

var (
	ErrEmptyPrint         = errors.New("a raw fingerprint has no items")
	ErrPrintLongerThanRef = errors.New("the preview's fingerprint is longer than the file's")
	ErrPreviewTooShort    = fmt.Errorf("the preview's fingerprint is under %d items, too short to tell songs apart", MinPreviewItems)
	ErrFileReachesCap     = errors.New("the file is as long as fpcalc fingerprints, so the preview may lie beyond it")
)

type Alignment struct {
	OffsetItems  int
	BitErrorRate float64
}

func OverlapSeconds(items int) float64 {
	return float64(items) * itemDurationSeconds
}

func (a Alignment) OffsetSeconds() float64 {
	return float64(a.OffsetItems) * itemDurationSeconds
}

func BestAlignment(file, preview []uint32) (Alignment, error) {
	switch {
	case len(file) == 0 || len(preview) == 0:
		return Alignment{}, ErrEmptyPrint
	case len(preview) > len(file):
		return Alignment{}, ErrPrintLongerThanRef
	}
	best := Alignment{BitErrorRate: bitErrorRate(file[:len(preview)], preview)}
	for offset := 1; offset+len(preview) <= len(file); offset++ {
		if rate := bitErrorRate(file[offset:offset+len(preview)], preview); rate < best.BitErrorRate {
			best = Alignment{OffsetItems: offset, BitErrorRate: rate}
		}
	}
	return best, nil
}

func bitErrorRate(window, preview []uint32) float64 {
	differing := 0
	for i, item := range preview {
		differing += bits.OnesCount32(window[i] ^ item)
	}
	return float64(differing) / float64(len(preview)*bitsPerItem)
}

func Incomparable(filePrint, previewPrint []uint32) string {
	switch {
	case len(previewPrint) < MinPreviewItems:
		return ErrPreviewTooShort.Error()
	case len(filePrint) >= FingerprintedItemCap:
		return ErrFileReachesCap.Error()
	case len(previewPrint) > len(filePrint):
		return ErrPrintLongerThanRef.Error()
	}
	return ""
}
