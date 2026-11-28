package service

import (
	"altune/go-api/internal/download/internal/domain"
	"strings"
)

const (
	ChannelTopic  = "topic"
	ChannelArtist = "artist"
	ChannelOther  = "other"
)

const topicChannelSuffix = "- Topic"

func UploaderOf(trackArtist string, c domain.AudioCandidate) string {
	if c.Kind == domain.KindCatalogPin {
		return ChannelTopic
	}
	switch {
	case isCreditedKind(c.Kind):
		return creditedClass(trackArtist, c.Artists)
	case artistMatchesChannel(trackArtist, c.Channel) && strings.HasSuffix(c.Channel, topicChannelSuffix):
		return ChannelTopic
	case artistMatchesChannel(trackArtist, c.Channel):
		return ChannelArtist
	default:
		return ChannelOther
	}
}

func artistMatchesCandidate(trackArtist string, c domain.AudioCandidate) bool {
	if c.Kind == domain.KindCatalogPin {
		return true
	}
	if isCreditedKind(c.Kind) {
		return domain.Credits(c.Artists, trackArtist)
	}
	return artistMatchesChannel(trackArtist, c.Channel)
}

func isCreditedKind(kind domain.CandidateKind) bool {
	return kind == domain.KindArtTrack || kind == domain.KindPinnedVideo
}

func creditedClass(trackArtist string, credited []string) string {
	if domain.Credits(credited, trackArtist) {
		return ChannelTopic
	}
	return ChannelOther
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
	for _, part := range domain.SplitCredit(spelled) {
		names = append(names, domain.CreditKey(part))
	}
	return names
}

func compactCreditKey(name string) string {
	return strings.ReplaceAll(domain.CreditKey(name), " ", "")
}
