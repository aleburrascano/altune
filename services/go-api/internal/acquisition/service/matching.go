package service

import (
	"log/slog"
	"math"
	"sort"
	"strings"

	discoverySvc "altune/go-api/internal/discovery/service"
)

const (
	identityMin   = 60.0
	durationTight = 3
	durationLoose = 15
)

func identityScore(trackTitle, trackArtist, candidateTitle string) float64 {
	combined := discoverySvc.NormalizeForMatch(trackArtist + " " + trackTitle)
	titleOnly := discoverySvc.NormalizeForMatch(trackTitle)
	candidateNorm := discoverySvc.NormalizeForMatch(candidateTitle)

	combinedScore := discoverySvc.TokenSortRatio(combined, candidateNorm)
	titleOnlyScore := discoverySvc.TokenSortRatio(titleOnly, candidateNorm)

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
	artistNorm := strings.ReplaceAll(discoverySvc.NormalizeForMatch(trackArtist), " ", "")
	channelNorm := strings.ReplaceAll(discoverySvc.NormalizeForMatch(channel), " ", "")
	return strings.Contains(channelNorm, artistNorm)
}

func SelectBestCandidate(track TrackRef, candidates []Candidate) *Candidate {
	if len(candidates) == 0 {
		return nil
	}

	var maxViews int64
	for _, c := range candidates {
		if c.ViewCount > maxViews {
			maxViews = c.ViewCount
		}
	}

	type topicEntry struct {
		ident        float64
		artistMatch  bool
		candidate    Candidate
	}
	type otherEntry struct {
		meta      float64
		ident     float64
		candidate Candidate
	}

	var topicCandidates []topicEntry
	var otherCandidates []otherEntry

	for _, c := range candidates {
		ident := identityScore(track.Title, track.Artist, c.Title)
		meta := metadataRank(c, track.Duration, maxViews)
		artMatch := artistMatchesChannel(track.Artist, c.Channel)

		slog.Info("candidate_evaluated",
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
			topicCandidates = append(topicCandidates, topicEntry{ident, artMatch, c})
		} else {
			otherCandidates = append(otherCandidates, otherEntry{meta, ident, c})
		}
	}

	if len(topicCandidates) > 0 {
		// Prefer Topic channels where the channel matches the expected artist.
		sort.Slice(topicCandidates, func(i, j int) bool {
			if topicCandidates[i].artistMatch != topicCandidates[j].artistMatch {
				return topicCandidates[i].artistMatch
			}
			return topicCandidates[i].ident > topicCandidates[j].ident
		})
		best := topicCandidates[0].candidate
		slog.Info("candidate_selected", "title", best.Title, "channel", best.Channel, "source", "topic_channel")
		return &best
	}

	if len(otherCandidates) > 0 {
		sort.Slice(otherCandidates, func(i, j int) bool {
			if otherCandidates[i].ident != otherCandidates[j].ident {
				return otherCandidates[i].ident > otherCandidates[j].ident
			}
			return otherCandidates[i].meta > otherCandidates[j].meta
		})
		best := otherCandidates[0].candidate
		slog.Info("candidate_selected", "title", best.Title, "channel", best.Channel,
			"metadata_rank", math.Round(otherCandidates[0].meta*1000)/1000, "source", "metadata_rank")
		return &best
	}

	slog.Warn("no_candidates_passed",
		"track_title", track.Title,
		"track_artist", track.Artist,
		"total_candidates", len(candidates),
	)
	return nil
}
