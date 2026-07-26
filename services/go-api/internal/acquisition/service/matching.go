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

var featuredRe = regexp.MustCompile(`(?i)\b(?:featuring|feat|ft)\.?\s+([^()\[\]]+)`)

var featSepRe = regexp.MustCompile(`(?i)\s*(?:,|&|\band\b)\s*`)

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

	durationMatchSlackSecs = 15.0
	durationMatchFraction  = 0.07

	authoritativeSlackSecs = 5.0
	authoritativeFraction  = 0.03
)

func identityScore(trackTitle, trackArtist, candidateTitle string) float64 {
	combined := textnorm.NormalizeForMatch(trackArtist + " " + trackTitle)
	titleOnly := textnorm.NormalizeForMatch(trackTitle)
	candidateNorm := textnorm.NormalizeForMatch(candidateTitle)

	combinedScore := textnorm.TokenSortRatio(combined, candidateNorm)
	titleOnlyScore := textnorm.TokenSortRatio(titleOnly, candidateNorm)

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
	artistNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(trackArtist), " ", "")
	channelNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(channel), " ", "")
	return strings.Contains(channelNorm, artistNorm)
}

type candidateEntry struct {
	ident         float64
	meta          float64
	durationDelta float64
	artistMatch   bool
	featMatch     bool
	candidate     ports.AudioCandidate
}

func durationDelta(expected, actual float64) float64 {
	if expected <= 0 || actual <= 0 {
		return math.MaxFloat64
	}
	return math.Abs(expected - actual)
}

func breakTie(a, b candidateEntry) bool {
	if a.durationDelta != b.durationDelta {
		return a.durationDelta < b.durationDelta
	}
	return a.candidate.URL < b.candidate.URL
}

func rankCandidates(ctx context.Context, track TrackRef, candidates []ports.AudioCandidate) []ports.AudioCandidate {
	if len(candidates) == 0 {
		return nil
	}

	maxViews := maxViewCount(candidates)
	resolved, topic, other := classifyCandidates(ctx, track, candidates, maxViews)

	sort.SliceStable(resolved, func(i, j int) bool {
		return breakTie(resolved[i], resolved[j])
	})

	sort.SliceStable(topic, func(i, j int) bool {
		if topic[i].artistMatch != topic[j].artistMatch {
			return topic[i].artistMatch
		}
		if topic[i].featMatch != topic[j].featMatch {
			return topic[i].featMatch
		}
		if topic[i].ident != topic[j].ident {
			return topic[i].ident > topic[j].ident
		}
		return breakTie(topic[i], topic[j])
	})
	sort.SliceStable(other, func(i, j int) bool {
		if other[i].ident != other[j].ident {
			return other[i].ident > other[j].ident
		}
		if other[i].featMatch != other[j].featMatch {
			return other[i].featMatch
		}
		if other[i].meta != other[j].meta {
			return other[i].meta > other[j].meta
		}
		return breakTie(other[i], other[j])
	})

	ranked := make([]ports.AudioCandidate, 0, len(resolved)+len(topic)+len(other))
	for _, bucket := range [][]candidateEntry{resolved, topic, other} {
		for _, e := range bucket {
			ranked = append(ranked, e.candidate)
		}
	}
	return ranked
}

func durationWithinTolerance(expected, actual float64) bool {
	tolerance := math.Max(durationMatchSlackSecs, expected*durationMatchFraction)
	return math.Abs(expected-actual) <= tolerance
}

func durationWithinAuthoritativeTolerance(expected, actual float64) bool {
	tolerance := math.Max(authoritativeSlackSecs, expected*authoritativeFraction)
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

func classifyCandidates(
	ctx context.Context,
	track TrackRef,
	candidates []ports.AudioCandidate,
	maxViews int64,
) (resolved, topic, other []candidateEntry) {

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

		entry := candidateEntry{
			ident:         ident,
			meta:          meta,
			durationDelta: durationDelta(track.Duration, c.Duration),
			artistMatch:   artMatch,
			featMatch:     featMatch,
			candidate:     c,
		}

		if c.Resolved {
			resolved = append(resolved, entry)
			continue
		}
		if ident < identityMin {
			continue
		}
		if isTopicChannel(c.Channel) {
			topic = append(topic, entry)
		} else {
			other = append(other, entry)
		}
	}

	return resolved, topic, other
}
