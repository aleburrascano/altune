package domain

import "unicode"

// This file finds grapheme-cluster boundaries (what a reader sees as one
// character) so truncation never cuts one in half. It implements the UAX #29
// rules that matter for user text with the standard library alone: CR LF
// (GB3), combining marks and other extenders (GB9, GB9a), prepended marks
// (GB9b), emoji ZWJ sequences (GB11), flag pairs (GB12/GB13), and Hangul jamo
// syllables (GB6-GB8). A cut directly after a ZWJ is always refused, which is
// stricter than GB11 but never leaves a dangling joiner. Every other boundary
// it finds is also a UAX #29 boundary, so it never splits a cluster.

const zwjRune rune = 0x200D

// clusterBoundaryAtOrBefore returns the largest boundary index <= cut, where
// index i means "between runes[i-1] and runes[i]". cut must be < len(runes).
func clusterBoundaryAtOrBefore(runes []rune, cut int) int {
	for cut > 0 && !isClusterBoundary(runes, cut) {
		cut--
	}
	return cut
}

// isClusterBoundary reports whether a cluster may end between runes[i-1] and
// runes[i], for 0 < i < len(runes).
func isClusterBoundary(runes []rune, i int) bool {
	prev, next := runes[i-1], runes[i]
	switch {
	case prev == '\r' && next == '\n', extendsCluster(next), prev == zwjRune,
		unicode.Is(prependRunes, prev), hangulJoins(prev, next):
		return false
	case isRegionalIndicator(next):
		return regionalIndicatorRunBefore(runes, i)%2 == 0
	}
	return true
}

// extendsCluster reports whether r attaches to the rune before it: a
// Grapheme_Extend rune (nonspacing/enclosing marks, variation selectors, ZWNJ,
// emoji tags), an emoji skin-tone modifier, a ZWJ, or a spacing mark (Mc plus
// the Thai and Lao SARA AM, which are Lo).
func extendsCluster(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc, unicode.Other_Grapheme_Extend) ||
		r == zwjRune || r == 0x0E33 || r == 0x0EB3 || (r >= 0x1F3FB && r <= 0x1F3FF)
}

// prependRunes is Grapheme_Cluster_Break=Prepend: marks such as the Arabic
// number sign that attach to the rune after them.
var prependRunes = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x0600, Hi: 0x0605, Stride: 1},
		{Lo: 0x06DD, Hi: 0x06DD, Stride: 1},
		{Lo: 0x070F, Hi: 0x070F, Stride: 1},
		{Lo: 0x0890, Hi: 0x0891, Stride: 1},
		{Lo: 0x08E2, Hi: 0x08E2, Stride: 1},
		{Lo: 0x0D4E, Hi: 0x0D4E, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x110BD, Hi: 0x110BD, Stride: 1},
		{Lo: 0x110CD, Hi: 0x110CD, Stride: 1},
		{Lo: 0x111C2, Hi: 0x111C3, Stride: 1},
		{Lo: 0x1193F, Hi: 0x1193F, Stride: 1},
		{Lo: 0x11941, Hi: 0x11941, Stride: 1},
		{Lo: 0x11A3A, Hi: 0x11A3A, Stride: 1},
		{Lo: 0x11A84, Hi: 0x11A89, Stride: 1},
		{Lo: 0x11D46, Hi: 0x11D46, Stride: 1},
		{Lo: 0x11F02, Hi: 0x11F02, Stride: 1},
	},
}

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// regionalIndicatorRunBefore counts the regional indicators immediately before
// index i; an odd count means runes[i] completes a flag pair.
func regionalIndicatorRunBefore(runes []rune, i int) int {
	n := 0
	for i-n > 0 && isRegionalIndicator(runes[i-n-1]) {
		n++
	}
	return n
}

// hangulJoins reports whether two Hangul runes belong to one syllable: a
// leading consonant (L) takes L, a vowel (V), or a syllable; a vowel or LV
// syllable takes V or a trailing consonant (T); a T or LVT syllable takes T.
func hangulJoins(prev, next rune) bool {
	switch {
	case isJamoL(prev):
		return isJamoL(next) || isJamoV(next) || isHangulSyllable(next)
	case isJamoV(prev) || isLVSyllable(prev):
		return isJamoV(next) || isJamoT(next)
	case isJamoT(prev) || isHangulSyllable(prev):
		return isJamoT(next)
	}
	return false
}

func isJamoL(r rune) bool { return (r >= 0x1100 && r <= 0x115F) || (r >= 0xA960 && r <= 0xA97C) }
func isJamoV(r rune) bool { return (r >= 0x1160 && r <= 0x11A7) || (r >= 0xD7B0 && r <= 0xD7C6) }
func isJamoT(r rune) bool { return (r >= 0x11A8 && r <= 0x11FF) || (r >= 0xD7CB && r <= 0xD7FB) }

const (
	hangulSyllableFirst = 0xAC00
	hangulSyllableLast  = 0xD7A3
	hangulTrailingCount = 28
)

func isHangulSyllable(r rune) bool { return r >= hangulSyllableFirst && r <= hangulSyllableLast }

// isLVSyllable reports a precomposed syllable with no trailing consonant.
func isLVSyllable(r rune) bool {
	return isHangulSyllable(r) && (r-hangulSyllableFirst)%hangulTrailingCount == 0
}
