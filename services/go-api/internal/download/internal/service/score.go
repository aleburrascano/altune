package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
	"math"
	"slices"
	"strings"
)

const (
	identityMin        = 60.0
	titleOnlyPenalty   = 0.6
	musicCategory      = "Music"
	unknownSignalScore = 0.5

	channelWeight  = 0.45
	lengthWeight   = 0.25
	categoryWeight = 0.20
	viewsWeight    = 0.10

	vevoChannelScore  = 0.8
	otherChannelScore = 0.3
	nonMusicScore     = 0.2
	looseLengthScore  = 0.5
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
	candidate      domain.AudioCandidate
}

func score(track RankTrack, c domain.AudioCandidate, maxViews int64) entry {
	uploader := UploaderOf(track.Artist, c)
	return entry{
		ident:         identityScore(track.Title, track.Artist, c.Title),
		meta:          metadataRank(c, uploader, track.ExpectedLength, maxViews),
		durationDelta: durationDelta(track.ExpectedLength, c.Duration),
		qualDistance:  qualifierDistance(track.Title, c.Title),
		artistMatch:   artistMatchesCandidate(track.Artist, c),
		uploader:      uploader,
		featMatch:     featureMatch(track.Title, c.Title),
		explicitFits:  c.Explicit == domain.PrefersExplicit(track.CatalogExplicit),
		candidate:     c,
	}
}

func maxViewCount(candidates []domain.AudioCandidate) int64 {
	var maxViews int64
	for _, c := range candidates {
		maxViews = max(maxViews, c.ViewCount)
	}
	return maxViews
}

func identityScore(trackTitle, trackArtist, candidateTitle string) float64 {
	candidate := music.NormalizeForMatch(candidateTitle)
	combined := music.TokenSortRatio(music.NormalizeForMatch(trackArtist+" "+trackTitle), candidate)
	titleOnly := music.TokenSortRatio(music.NormalizeForMatch(trackTitle), candidate)
	return math.Max(combined, titleOnly*titleOnlyPenalty)
}

func metadataRank(c domain.AudioCandidate, uploader string, expectedLength float64, maxViews int64) float64 {
	channel := channelScore(uploader, c.Channel)
	category := categoryScore(c.Categories)
	length := durationScore(expectedLength, c.Duration)
	views := viewScore(c.ViewCount, maxViews)
	return channelWeight*channel + lengthWeight*length + categoryWeight*category + viewsWeight*views
}

func channelScore(uploader, channel string) float64 {
	if uploader == ChannelTopic || uploader == ChannelArtist {
		return 1.0
	}
	if strings.Contains(strings.ToLower(channel), "vevo") {
		return vevoChannelScore
	}
	return otherChannelScore
}

func categoryScore(categories []string) float64 {
	if slices.Contains(categories, musicCategory) {
		return 1.0
	}
	return nonMusicScore
}

func durationScore(expected, actual float64) float64 {
	if expected == 0 || actual == 0 {
		return unknownSignalScore
	}
	switch {
	case domain.LengthTight.Agrees(expected, actual):
		return 1.0
	case domain.LengthLoose.Agrees(expected, actual):
		return looseLengthScore
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
