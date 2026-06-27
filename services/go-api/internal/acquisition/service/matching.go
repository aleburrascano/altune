package service

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"

	"altune/go-api/internal/shared/textnorm"
)

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

func metadataRank(c Candidate, expectedDuration float64, maxViews int64) float64 {
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
	candidate   Candidate
}

type otherEntry struct {
	meta      float64
	ident     float64
	candidate Candidate
}

// SelectBestCandidate is the context-less entry point retained for tests and
// callers without a request context. Production calls selectBestCandidate with a
// context so candidate-evaluation logs carry the correlation id.
func SelectBestCandidate(track TrackRef, candidates []Candidate) *Candidate {
	return selectBestCandidate(context.Background(), track, candidates)
}

func selectBestCandidate(ctx context.Context, track TrackRef, candidates []Candidate) *Candidate {
	ranked := rankCandidates(ctx, track, candidates)
	if len(ranked) == 0 {
		slog.WarnContext(ctx, "no_candidates_passed",
			"track_title", track.Title,
			"track_artist", track.Artist,
			"total_candidates", len(candidates),
		)
		return nil
	}
	best := ranked[0]
	return &best
}

// rankCandidates returns every identity-passing candidate ordered best-first.
// Topic-channel candidates rank ahead of all others (artist-matching Topic first,
// then by identity); non-Topic candidates follow, ordered by identity then
// metadata. Selection takes element 0; the acquisition pipeline walks the rest as
// fallbacks when a downloaded file fails duration verification.
func rankCandidates(ctx context.Context, track TrackRef, candidates []Candidate) []Candidate {
	if len(candidates) == 0 {
		return nil
	}

	maxViews := maxViewCount(candidates)
	topic, other := classifyCandidates(ctx, track, candidates, maxViews)

	sort.Slice(topic, func(i, j int) bool {
		if topic[i].artistMatch != topic[j].artistMatch {
			return topic[i].artistMatch
		}
		return topic[i].ident > topic[j].ident
	})
	sort.Slice(other, func(i, j int) bool {
		if other[i].ident != other[j].ident {
			return other[i].ident > other[j].ident
		}
		return other[i].meta > other[j].meta
	})

	ranked := make([]Candidate, 0, len(topic)+len(other))
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

func maxViewCount(candidates []Candidate) int64 {
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
func classifyCandidates(ctx context.Context, track TrackRef, candidates []Candidate, maxViews int64) ([]topicEntry, []otherEntry) {
	var topic []topicEntry
	var other []otherEntry

	for _, c := range candidates {
		ident := identityScore(track.Title, track.Artist, c.Title)
		meta := metadataRank(c, track.Duration, maxViews)
		artMatch := artistMatchesChannel(track.Artist, c.Channel)

		slog.InfoContext(ctx, "candidate_evaluated",
			"candidate_title", c.Title,
			"candidate_channel", c.Channel,
			"candidate_duration", c.Duration,
			"candidate_views", c.ViewCount,
			"identity_score", math.Round(ident*10)/10,
			"metadata_rank", math.Round(meta*1000)/1000,
			"is_topic", isTopicChannel(c.Channel),
			"artist_match", artMatch,
			"track_artist", track.Artist,
		)

		if ident < identityMin {
			continue
		}

		if isTopicChannel(c.Channel) {
			topic = append(topic, topicEntry{ident: ident, artistMatch: artMatch, candidate: c})
		} else {
			other = append(other, otherEntry{meta: meta, ident: ident, candidate: c})
		}
	}

	return topic, other
}

