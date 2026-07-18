package service

import (
	"context"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
)

// featuredRe captures the featured-artist blob after a feat/ft/featuring marker,
// up to a bracket or end of string. "with" is deliberately excluded — it mangles
// real titles like "Stuck with U" (the same reason textnorm dropped it).
var featuredRe = regexp.MustCompile(`(?i)\b(?:featuring|feat|ft)\.?\s+([^()\[\]]+)`)

// featSepRe splits a featured-artist blob into individual names ("A & B", "A, B").
var featSepRe = regexp.MustCompile(`(?i)\s*(?:,|&|\band\b)\s*`)

// extractFeaturedArtists pulls the featured-artist names out of a title's
// "feat./ft./featuring X" marker. Returns nil when the title carries no feature.
// NOTE: NormalizeForMatch strips bracketed segments, so by the time identityScore
// compares titles the "(feat. X)" is gone — the matcher is blind to features. This
// reads the RAW title to recover that lost signal for feature-aware ranking.
func extractFeaturedArtists(title string) []string {
	m := featuredRe.FindStringSubmatch(title)
	if m == nil {
		return nil
	}
	parts := featSepRe.Split(strings.TrimSpace(m[1]), -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// featureMatch reports whether a candidate is consistent with the track's feature.
// When the track names featured artists, a candidate must mention every one of
// them in its (raw, un-normalized) title — otherwise it is a different recording
// (the solo cut / official video that the duration-blind identity score cannot
// tell apart). A track with no feature imposes no requirement (every candidate
// passes), so solo-track acquisition is unaffected.
func featureMatch(trackTitle, candidateTitle string) bool {
	feats := extractFeaturedArtists(trackTitle)
	if len(feats) == 0 {
		return true
	}
	cand := strings.ToLower(candidateTitle)
	for _, f := range feats {
		if !strings.Contains(cand, strings.ToLower(f)) {
			return false
		}
	}
	return true
}

const (
	identityMin   = 60.0
	durationTight = 3
	durationLoose = 15

	// Same-recording tolerance for post-download verification (durationWithinTolerance):
	// the larger of an absolute slack and a fraction of the expected length. The same
	// recording from any source runs the same length within a few seconds (intro/outro
	// or silence trimming); a larger gap means a different recording.
	durationMatchSlackSecs = 15.0
	durationMatchFraction  = 0.07
)

func identityScore(trackTitle, trackArtist, candidateTitle string) float64 {
	combined := textnorm.NormalizeForMatch(trackArtist + " " + trackTitle)
	titleOnly := textnorm.NormalizeForMatch(trackTitle)
	candidateNorm := textnorm.NormalizeForMatch(candidateTitle)

	combinedScore := textnorm.TokenSortRatio(combined, candidateNorm)
	titleOnlyScore := textnorm.TokenSortRatio(titleOnly, candidateNorm)

	// Penalize title-only matches: without artist context the match is
	// ambiguous (e.g., "Die Hard" matches Kendrick's "DIE HARD" by title
	// alone). The penalty discourages selecting a wrong-artist candidate
	// when a combined artist+title match exists.
	titleOnlyScore *= 0.6

	return math.Max(combinedScore, titleOnlyScore)
}

func channelScore(channel string) float64 {
	if strings.HasSuffix(channel, "- Topic") {
		return 1.0
	}
	if strings.Contains(strings.ToLower(channel), "vevo") {
		return 0.8
	}
	return 0.3
}

func categoryScore(categories []string) float64 {
	for _, c := range categories {
		if c == "Music" {
			return 1.0
		}
	}
	return 0.2
}

func durationScore(expected, actual float64) float64 {
	if expected == 0 || actual == 0 {
		return 0.5
	}
	diff := math.Abs(expected - actual)
	if diff <= durationTight {
		return 1.0
	}
	if diff <= durationLoose {
		return 0.5
	}
	return 0.0
}

func viewScore(viewCount, maxViews int64) float64 {
	if maxViews == 0 {
		return 0.5
	}
	return math.Min(float64(viewCount)/float64(maxViews), 1.0)
}

func metadataRank(c ports.AudioCandidate, expectedDuration float64, maxViews int64) float64 {
	ch := channelScore(c.Channel)
	cat := categoryScore(c.Categories)
	dur := durationScore(expectedDuration, c.Duration)
	views := viewScore(c.ViewCount, maxViews)
	return 0.45*ch + 0.25*dur + 0.20*cat + 0.10*views
}

func isTopicChannel(channel string) bool {
	return strings.HasSuffix(channel, "- Topic")
}

func artistMatchesChannel(trackArtist, channel string) bool {
	// Strip spaces so spaceless VEVO/official channels ("TheWeekndVEVO") still
	// match the spaced artist name ("The Weeknd").
	artistNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(trackArtist), " ", "")
	channelNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(channel), " ", "")
	return strings.Contains(channelNorm, artistNorm)
}

type topicEntry struct {
	ident       float64
	artistMatch bool
	featMatch   bool
	candidate   ports.AudioCandidate
}

type otherEntry struct {
	meta      float64
	ident     float64
	featMatch bool
	candidate ports.AudioCandidate
}

// rankCandidates returns every identity-passing candidate ordered best-first.
// Topic-channel candidates rank ahead of all others (artist-matching Topic first,
// then by identity); non-Topic candidates follow, ordered by identity then
// metadata. Selection takes element 0; the acquisition pipeline walks the rest as
// fallbacks when a downloaded file fails duration verification.
func rankCandidates(ctx context.Context, track TrackRef, candidates []ports.AudioCandidate) []ports.AudioCandidate {
	if len(candidates) == 0 {
		return nil
	}

	maxViews := maxViewCount(candidates)
	topic, other := classifyCandidates(ctx, track, candidates, maxViews)

	sort.Slice(topic, func(i, j int) bool {
		if topic[i].artistMatch != topic[j].artistMatch {
			return topic[i].artistMatch
		}
		if topic[i].featMatch != topic[j].featMatch {
			return topic[i].featMatch
		}
		return topic[i].ident > topic[j].ident
	})
	sort.Slice(other, func(i, j int) bool {
		if other[i].ident != other[j].ident {
			return other[i].ident > other[j].ident
		}
		// Feature-consistency breaks an identity tie: a "(feat. X)" track prefers a
		// candidate that names X over an equally-scored solo cut / official video
		// (identity can't see the stripped "(feat. X)"). See featureMatch.
		if other[i].featMatch != other[j].featMatch {
			return other[i].featMatch
		}
		return other[i].meta > other[j].meta
	})

	ranked := make([]ports.AudioCandidate, 0, len(topic)+len(other))
	for _, e := range topic {
		ranked = append(ranked, e.candidate)
	}
	for _, e := range other {
		ranked = append(ranked, e.candidate)
	}
	return ranked
}

// durationWithinTolerance reports whether an acquired file's actual duration is
// close enough to the track's expected (catalog-provider) duration to be the same
// recording. Callers must only invoke it when the expected duration is known.
func durationWithinTolerance(expected, actual float64) bool {
	tolerance := math.Max(durationMatchSlackSecs, expected*durationMatchFraction)
	return math.Abs(expected-actual) <= tolerance
}

func maxViewCount(candidates []ports.AudioCandidate) int64 {
	var maxViews int64
	for _, c := range candidates {
		if c.ViewCount > maxViews {
			maxViews = c.ViewCount
		}
	}
	return maxViews
}

// classifyCandidates scores every candidate, drops those below the identity
// gate, and splits the survivors into Topic-channel and other buckets.
func classifyCandidates(ctx context.Context, track TrackRef, candidates []ports.AudioCandidate, maxViews int64) ([]topicEntry, []otherEntry) {
	var topic []topicEntry
	var other []otherEntry

	for _, c := range candidates {
		ident := identityScore(track.Title, track.Artist, c.Title)
		meta := metadataRank(c, track.Duration, maxViews)
		artMatch := artistMatchesChannel(track.Artist, c.Channel)
		featMatch := featureMatch(track.Title, c.Title)

		slog.InfoContext(ctx, "candidate_evaluated",
			"candidate_title", c.Title,
			"candidate_channel", c.Channel,
			"candidate_duration", c.Duration,
			"candidate_views", c.ViewCount,
			"identity_score", math.Round(ident*10)/10,
			"metadata_rank", math.Round(meta*1000)/1000,
			"is_topic", isTopicChannel(c.Channel),
			"artist_match", artMatch,
			"feature_match", featMatch,
			"track_artist", track.Artist,
		)

		if ident < identityMin {
			continue
		}

		if isTopicChannel(c.Channel) {
			topic = append(topic, topicEntry{ident: ident, artistMatch: artMatch, featMatch: featMatch, candidate: c})
		} else {
			other = append(other, otherEntry{meta: meta, ident: ident, featMatch: featMatch, candidate: c})
		}
	}

	return topic, other
}

