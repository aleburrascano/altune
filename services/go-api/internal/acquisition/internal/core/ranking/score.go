package ranking

import (
	"altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"slices"
	"strings"
)

const (
	identityMin        = 60.0
	titleOnlyPenalty   = 0.6
	musicCategory      = "Music"
	unknownSignalScore = 0.5
)

type entry struct {
	ident          float64
	meta           float64
	durationDelta  float64
	qualDistance   int
	isOtherVersion bool
	artistMatch    bool
	uploader       string
	featMatch      bool
	explicitFits   bool
	candidate      ports.AudioCandidate
}

func score(track Track, c ports.AudioCandidate, maxViews int64) entry {
	uploader := UploaderOf(track.Artist, c)
	return entry{
		ident:         identityScore(track.Title, track.Artist, c.Title),
		meta:          metadataRank(c, uploader, track.ExpectedLength, maxViews),
		durationDelta: durationDelta(track.ExpectedLength, c.Duration),
		qualDistance:  qualifierDistance(track.Title, c.Title),
		artistMatch:   artistMatchesCandidate(track.Artist, c),
		uploader:      uploader,
		featMatch:     featureMatch(track.Title, c.Title),
		explicitFits:  c.Explicit == ports.PrefersExplicit(track.CatalogExplicit),
		candidate:     c,
	}
}

func maxViewCount(candidates []ports.AudioCandidate) int64 {
	var maxViews int64
	for _, c := range candidates {
		maxViews = max(maxViews, c.ViewCount)
	}
	return maxViews
}

func identityScore(trackTitle, trackArtist, candidateTitle string) float64 {
	candidate := textnorm.NormalizeForMatch(candidateTitle)
	combined := textnorm.TokenSortRatio(textnorm.NormalizeForMatch(trackArtist+" "+trackTitle), candidate)
	titleOnly := textnorm.TokenSortRatio(textnorm.NormalizeForMatch(trackTitle), candidate)
	return math.Max(combined, titleOnly*titleOnlyPenalty)
}

func metadataRank(c ports.AudioCandidate, uploader string, expectedLength float64, maxViews int64) float64 {
	channel := channelScore(uploader, c.Channel)
	category := categoryScore(c.Categories)
	length := durationScore(expectedLength, c.Duration)
	views := viewScore(c.ViewCount, maxViews)
	return 0.45*channel + 0.25*length + 0.20*category + 0.10*views
}

func channelScore(uploader, channel string) float64 {
	if uploader == ChannelTopic || uploader == ChannelArtist {
		return 1.0
	}
	if strings.Contains(strings.ToLower(channel), "vevo") {
		return 0.8
	}
	return 0.3
}

func categoryScore(categories []string) float64 {
	if slices.Contains(categories, musicCategory) {
		return 1.0
	}
	return 0.2
}

func durationScore(expected, actual float64) float64 {
	if expected == 0 || actual == 0 {
		return unknownSignalScore
	}
	switch {
	case domain.LengthTight.Agrees(expected, actual):
		return 1.0
	case domain.LengthLoose.Agrees(expected, actual):
		return 0.5
	default:
		return 0.0
	}
}

func viewScore(viewCount, maxViews int64) float64 {
	if maxViews == 0 {
		return unknownSignalScore
	}
	return float64(viewCount) / float64(maxViews)
}

func durationDelta(expected, actual float64) float64 {
	if expected <= 0 || actual <= 0 {
		return math.MaxFloat64
	}
	return math.Abs(expected - actual)
}
