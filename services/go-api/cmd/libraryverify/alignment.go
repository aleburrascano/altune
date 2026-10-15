package main

import (
	"errors"
	"fmt"
	"math/bits"
)

const (
	bitsPerItem         = 32
	itemDurationSeconds = 1365.0 / 11025.0
	minPreviewItems     = 100
)

var (
	errEmptyPrint         = errors.New("a raw fingerprint has no items")
	errPrintLongerThanRef = errors.New("the preview's fingerprint is longer than the file's")
	errPreviewTooShort    = fmt.Errorf("the preview's fingerprint is under %d items, too short to tell songs apart", minPreviewItems)
	errFileReachesCap     = errors.New("the file is as long as fpcalc fingerprints, so the preview may lie beyond it")
)

type alignment struct {
	OffsetItems  int
	BitErrorRate float64
}

func (a alignment) offsetSeconds() float64 {
	return float64(a.OffsetItems) * itemDurationSeconds
}

func bestAlignment(file, preview []uint32) (alignment, error) {
	switch {
	case len(file) == 0 || len(preview) == 0:
		return alignment{}, errEmptyPrint
	case len(preview) > len(file):
		return alignment{}, errPrintLongerThanRef
	}
	best := alignment{BitErrorRate: bitErrorRate(file[:len(preview)], preview)}
	for offset := 1; offset+len(preview) <= len(file); offset++ {
		if rate := bitErrorRate(file[offset:offset+len(preview)], preview); rate < best.BitErrorRate {
			best = alignment{OffsetItems: offset, BitErrorRate: rate}
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
