package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"strings"
)

const (
	unrequestedQualifierCost = 1
	unfulfilledQualifierCost = 2

	identityMin   = 60.0
	durationTight = 3
	durationLoose = 15
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
	if isTopicChannel(channel) {
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
	if artistNorm == "" {
		return false
	}
	channelNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(channel), " ", "")
	return strings.Contains(channelNorm, artistNorm)
}

type candidateEntry struct {
	ident         float64
	meta          float64
	durationDelta float64
	qualDistance  int
	isFallback    bool
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
