package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/audiojudge"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
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
	VerdictContradicted  VerdictKind = "contradicted"
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
	Judged    audiojudge.Verdict
}

func audioVerdictOf(judged audiojudge.Verdict, results []ports.AcoustIDResult) AudioVerdict {
	return AudioVerdict{Kind: verdictKindOf(judged.Kind), Score: topScore(results), Surviving: judged.Linked, Judged: judged}
}

func verdictKindOf(kind audiojudge.Kind) VerdictKind {
	switch kind {
	case audiojudge.Proven:
		return VerdictHard
	case audiojudge.Likely:
		return VerdictSoft
	case audiojudge.Contradicted:
		return VerdictContradicted
	}
	return VerdictUnknown
}

type LookedUpRecordings map[string]ports.RecordingFacts

func ClassifyAudio(ref AudioReference, audioDuration float64, results []ports.AcoustIDResult, facts LookedUpRecordings) AudioVerdict {
	surviving := recordingsMatchingLength(audioDuration, results)
	if len(surviving) == 0 {
		return AudioVerdict{Kind: VerdictUnknown}
	}
	return AudioVerdict{
		Kind:      newAudioMatcher(ref, audioDuration, facts).verdictFor(surviving),
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

func SoftCandidateMBIDs(ref AudioReference, audioDuration float64, results []ports.AcoustIDResult) []string {
	matcher := newAudioMatcher(ref, audioDuration, nil)
	var mbids []string
	for _, recording := range recordingsMatchingLength(audioDuration, results) {
		if recording.MBID != "" && !matcher.isReferenceRecording(recording) && matcher.agreesOnSongArtistAndLength(recording) && !slices.Contains(mbids, recording.MBID) {
			mbids = append(mbids, recording.MBID)
		}
	}
	return mbids
}

func sharesVersionLabels(trackTitle string, facts ports.RecordingFacts) bool {
	return slices.Equal(versiontag.RecordingLabels(trackTitle), versiontag.RecordingLabels(recordingTitleWithDisambiguation(facts)))
}

func recordingTitleWithDisambiguation(facts ports.RecordingFacts) string {
	if facts.Disambiguation == "" {
		return facts.Title
	}
	return facts.Title + " (" + facts.Disambiguation + ")"
}

type audioMatcher struct {
	facts        LookedUpRecordings
	mbids        []string
	core         string
	ref          AudioReference
	artists      map[string]bool
	lengthAgrees bool
}

func newAudioMatcher(ref AudioReference, audioDuration float64, facts LookedUpRecordings) audioMatcher {
	return audioMatcher{
		facts:        facts,
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
	case slices.ContainsFunc(surviving, m.isUnverifiedVersion):
		return VerdictUnknown
	default:
		return VerdictDifferentSong
	}
}

func (m audioMatcher) isReferenceRecording(recording ports.LinkedRecording) bool {
	return recording.MBID != "" && slices.Contains(m.mbids, recording.MBID)
}

func (m audioMatcher) agreesOnSongArtistAndLength(recording ports.LinkedRecording) bool {
	return m.lengthAgrees && m.isSameSong(recording) && !m.hasUnrequestedQualifier(recording) && m.sharesArtist(recording)
}

func (m audioMatcher) agreesSoftly(recording ports.LinkedRecording) bool {
	facts, found := m.facts[recording.MBID]
	return m.agreesOnSongArtistAndLength(recording) && found && sharesVersionLabels(m.ref.Title, facts)
}

func (m audioMatcher) isOtherVersion(recording ports.LinkedRecording) bool {
	if m.isSameSong(recording) && m.hasUnrequestedQualifier(recording) {
		return true
	}
	facts, found := m.facts[recording.MBID]
	return found && m.agreesOnSongArtistAndLength(recording) && !sharesVersionLabels(m.ref.Title, facts)
}

func (m audioMatcher) isUnverifiedVersion(recording ports.LinkedRecording) bool {
	_, found := m.facts[recording.MBID]
	return !found && m.agreesOnSongArtistAndLength(recording)
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
