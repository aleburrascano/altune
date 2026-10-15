package service

import (
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"strings"
)

const (
	unrequestedQualifierCost = 1
	unfulfilledQualifierCost = 2

	identityMin = 60.0
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

const topicChannelSuffix = "- Topic"

func isTopicChannel(channel string) bool {
	return strings.HasSuffix(channel, topicChannelSuffix)
}

func artistMatchesChannel(trackArtist, channel string) bool {
	name := compactCreditKey(strings.TrimSuffix(channel, topicChannelSuffix))
	for _, credited := range creditedArtists(trackArtist) {
		want := compactCreditKey(credited)
		if want != "" && (name == want || name == want+"vevo" || name == want+"official") {
			return true
		}
	}
	return false
}

func creditedArtists(credit string) []string {
	spelled := strings.ReplaceAll(credit, "$", "s")
	names := []string{spelled}
	for name := range artistNames(spelled) {
		names = append(names, name)
	}
	return names
}

func compactCreditKey(name string) string {
	return strings.ReplaceAll(domain.CreditKey(name), " ", "")
}

type candidateEntry struct {
	ident         float64
	meta          float64
	durationDelta float64
	qualDistance  int
	isFallback    bool
	artistMatch   bool
	uploader      string
	featMatch     bool
	explicitFits  bool
	candidate     ports.AudioCandidate
}

func durationDelta(expected, actual float64) float64 {
	if expected <= 0 || actual <= 0 {
		return math.MaxFloat64
	}
	return math.Abs(expected - actual)
}

func uploaderClass(trackArtist string, c ports.AudioCandidate) string {
	switch {
	case c.Kind == ports.KindCatalogPin:
		return channelTopic
	case isCreditedKind(c.Kind):
		return artTrackClass(trackArtist, c.Artists)
	case artistMatchesChannel(trackArtist, c.Channel) && isTopicChannel(c.Channel):
		return channelTopic
	case artistMatchesChannel(trackArtist, c.Channel):
		return channelArtist
	default:
		return channelOther
	}
}

func isCreditedKind(kind ports.CandidateKind) bool {
	return kind == ports.KindArtTrack || kind == ports.KindPinnedVideo
}

func artTrackClass(trackArtist string, credited []string) string {
	if domain.Credits(credited, trackArtist) {
		return channelTopic
	}
	return channelOther
}

func artistMatchesCandidate(trackArtist string, c ports.AudioCandidate) bool {
	if c.Kind == ports.KindCatalogPin {
		return true
	}
	if isCreditedKind(c.Kind) {
		return artTrackClass(trackArtist, c.Artists) == channelTopic
	}
	return artistMatchesChannel(trackArtist, c.Channel)
}
