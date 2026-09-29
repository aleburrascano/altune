package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"regexp"
	"slices"
)

type VerdictKind string

const (
	VerdictHard          VerdictKind = "hard"
	VerdictSoft          VerdictKind = "soft"
	VerdictOtherVersion  VerdictKind = "other_version"
	VerdictDifferentSong VerdictKind = "different_song"
	VerdictUnknown       VerdictKind = "unknown"
)

type AudioReference struct {
	Title    string
	Artist   string
	Duration float64
	MBIDs    []string
}

type AudioVerdict struct {
	Kind      VerdictKind
	Score     float64
	Surviving []ports.LinkedRecording
}

func ClassifyAudio(ref AudioReference, audioDuration float64, results []ports.AcoustIDResult) AudioVerdict {
	if len(results) == 0 {
		return AudioVerdict{Kind: VerdictUnknown}
	}
	surviving := recordingsMatchingLength(audioDuration, results)
	return AudioVerdict{
		Kind:      newAudioMatcher(ref, audioDuration).verdictFor(surviving),
		Score:     topScore(results),
		Surviving: surviving,
	}
}

func recordingsMatchingLength(audioDuration float64, results []ports.AcoustIDResult) []ports.LinkedRecording {
	var surviving []ports.LinkedRecording
	for _, result := range results {
		for _, recording := range result.Recordings {
			if linkLengthAgrees(audioDuration, recording.Duration) {
				surviving = append(surviving, recording)
			}
		}
	}
	return surviving
}

func linkLengthAgrees(audioDuration, linkDuration float64) bool {
	isLengthUnknown := linkDuration == 0 || audioDuration <= 0
	return isLengthUnknown || durationWithinAuthoritativeTolerance(audioDuration, linkDuration)
}

func topScore(results []ports.AcoustIDResult) float64 {
	var best float64
	seen := false
	for _, result := range results {
		if math.IsNaN(result.Score) {
			continue
		}
		if !seen || result.Score > best {
			best = result.Score
			seen = true
		}
	}
	return best
}

type audioMatcher struct {
	mbids        []string
	core         string
	ref          AudioReference
	artists      map[string]bool
	lengthAgrees bool
}

func newAudioMatcher(ref AudioReference, audioDuration float64) audioMatcher {
	return audioMatcher{
		mbids:        ref.MBIDs,
		core:         coreTitle(ref.Title),
		ref:          ref,
		artists:      artistNames(ref.Artist),
		lengthAgrees: referenceLengthAgrees(ref.Duration, audioDuration),
	}
}

func referenceLengthAgrees(refDuration, audioDuration float64) bool {
	if refDuration == 0 {
		return true
	}
	if refDuration < 0 {
		return false
	}
	return audioDuration > 0 && durationWithinAuthoritativeTolerance(audioDuration, refDuration)
}

func (m audioMatcher) verdictFor(surviving []ports.LinkedRecording) VerdictKind {
	switch {
	case slices.ContainsFunc(surviving, m.isReferenceRecording):
		return VerdictHard
	case slices.ContainsFunc(surviving, m.agreesSoftly) && !m.mostTitlesDiffer(surviving):
		return VerdictSoft
	case slices.ContainsFunc(surviving, m.isOtherVersion):
		return VerdictOtherVersion
	default:
		return VerdictDifferentSong
	}
}

func (m audioMatcher) isReferenceRecording(recording ports.LinkedRecording) bool {
	return recording.MBID != "" && slices.Contains(m.mbids, recording.MBID)
}

func (m audioMatcher) agreesSoftly(recording ports.LinkedRecording) bool {
	return m.lengthAgrees && m.isSameSong(recording) && !m.hasUnrequestedQualifier(recording) && m.sharesArtist(recording)
}

func (m audioMatcher) isOtherVersion(recording ports.LinkedRecording) bool {
	return m.isSameSong(recording) && m.hasUnrequestedQualifier(recording)
}

func (m audioMatcher) isSameSong(recording ports.LinkedRecording) bool {
	return m.core != "" && coreTitle(recording.Title) == m.core
}

func (m audioMatcher) hasUnrequestedQualifier(recording ports.LinkedRecording) bool {
	veto, _ := UnrequestedQualifiers(m.ref.Title, m.ref.Artist, recording.Title)
	return len(veto) > 0
}

func (m audioMatcher) sharesArtist(recording ports.LinkedRecording) bool {
	for _, credit := range recording.Artists {
		for name := range artistNames(credit) {
			if m.artists[name] {
				return true
			}
		}
	}
	return false
}

func (m audioMatcher) mostTitlesDiffer(surviving []ports.LinkedRecording) bool {
	titles := make(map[string]bool)
	for _, recording := range surviving {
		if core := coreTitle(recording.Title); core != "" {
			titles[core] = true
		}
	}
	differing := len(titles)
	if titles[m.core] {
		differing--
	}
	return differing*2 > len(titles)
}

func coreTitle(title string) string {
	return textnorm.NormalizeForMatch(title)
}

var artistSeparatorRe = regexp.MustCompile(`(?i)\s*(?:&|,|\b(?:featuring|feat|ft)\b\.?)\s*`)

func artistNames(credit string) map[string]bool {
	names := make(map[string]bool)
	for _, part := range artistSeparatorRe.Split(credit, -1) {
		if name := textnorm.NormalizeForMatch(part); name != "" {
			names[name] = true
		}
	}
	return names
}
