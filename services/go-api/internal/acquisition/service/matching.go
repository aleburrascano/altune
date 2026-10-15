package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
)

func breakTie(a, b candidateEntry) bool {
	if a.durationDelta != b.durationDelta {
		return a.durationDelta < b.durationDelta
	}
	return a.candidate.URL < b.candidate.URL
}

func lessResolved(a, b candidateEntry) bool {
	if a.explicitFits != b.explicitFits {
		return a.explicitFits
	}
	return breakTie(a, b)
}

func lessTopic(a, b candidateEntry) bool {
	if a.isFallback != b.isFallback {
		return b.isFallback
	}
	if a.artistMatch != b.artistMatch {
		return a.artistMatch
	}
	if a.featMatch != b.featMatch {
		return a.featMatch
	}
	if a.qualDistance != b.qualDistance {
		return a.qualDistance < b.qualDistance
	}
	if a.ident != b.ident {
		return a.ident > b.ident
	}
	return breakTie(a, b)
}

func lessOther(a, b candidateEntry) bool {
	if a.isFallback != b.isFallback {
		return b.isFallback
	}
	if a.ident != b.ident {
		return a.ident > b.ident
	}
	if a.featMatch != b.featMatch {
		return a.featMatch
	}
	if a.meta != b.meta {
		return a.meta > b.meta
	}
	if a.qualDistance != b.qualDistance {
		return a.qualDistance < b.qualDistance
	}
	return breakTie(a, b)
}

func rankAndCollect(ctx context.Context, track TrackRef, expectedLength float64, candidates []ports.AudioCandidate) ([]ports.AudioCandidate, []CandidateRejection) {
	if len(candidates) == 0 {
		return nil, nil
	}

	maxViews := maxViewCount(candidates)
	resolved, topic, other, rejected := classifyCandidates(ctx, track, expectedLength, candidates, maxViews)

	sortBucket(resolved, lessResolved)
	sortBucket(topic, lessTopic)
	sortBucket(other, lessOther)
	return flattenBuckets(resolved, topic, other), rejected
}

func sortBucket(bucket []candidateEntry, less func(a, b candidateEntry) bool) {
	sort.SliceStable(bucket, func(i, j int) bool { return less(bucket[i], bucket[j]) })
}

func flattenBuckets(buckets ...[]candidateEntry) []ports.AudioCandidate {
	var ranked []ports.AudioCandidate
	for _, bucket := range buckets {
		for _, e := range bucket {
			ranked = append(ranked, e.candidate)
		}
	}
	return ranked
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

func scoreCandidate(track TrackRef, expectedLength float64, c ports.AudioCandidate, maxViews int64) candidateEntry {
	return candidateEntry{
		ident:         identityScore(track.Title, track.Artist, c.Title),
		meta:          metadataRank(c, expectedLength, maxViews),
		durationDelta: durationDelta(expectedLength, c.Duration),
		qualDistance:  qualifierDistance(track.Title, c.Title),
		artistMatch:   artistMatchesCandidate(track.Artist, c),
		uploader:      uploaderClass(track.Artist, c),
		featMatch:     featureMatch(track.Title, c.Title),
		explicitFits:  c.Explicit == ports.PrefersExplicit(track.CatalogExplicit),
		candidate:     c,
	}
}

func logCandidateEvaluated(ctx context.Context, track TrackRef, e candidateEntry) {
	c := e.candidate
	slog.InfoContext(ctx, "candidate_evaluated",
		"track_id", track.ID,
		"source", c.Source,
		"candidate_title", c.Title,
		"candidate_channel", c.Channel,
		"candidate_duration", c.Duration,
		"candidate_views", c.ViewCount,
		"identity_score", math.Round(e.ident*10)/10,
		"metadata_rank", math.Round(e.meta*1000)/1000,
		"qualifier_distance", e.qualDistance,
		"is_topic", e.uploader == channelTopic,
		"artist_match", e.artistMatch,
		"feature_match", e.featMatch,
		"explicit", c.Explicit,
		"track_artist", track.Artist,
	)
}

func qualifierRejection(c ports.AudioCandidate, veto []string) CandidateRejection {
	return CandidateRejection{
		URL:    c.URL,
		Title:  c.Title,
		Source: c.Source,
		Stage:  RejectionQualifier,
		Reason: "unrequested " + strings.Join(veto, ", "),
	}
}

func unplayableRejection(c ports.AudioCandidate) CandidateRejection {
	return CandidateRejection{
		URL:    c.URL,
		Title:  c.Title,
		Source: c.Source,
		Stage:  unplayableStage(c.Unplayable),
		Reason: "soundcloud " + c.Unplayable,
	}
}

func classifyCandidates(
	ctx context.Context,
	track TrackRef,
	expectedLength float64,
	candidates []ports.AudioCandidate,
	maxViews int64,
) (resolved, topic, other []candidateEntry, rejected []CandidateRejection) {
	for _, c := range candidates {
		if c.Unplayable != "" {
			rejected = append(rejected, unplayableRejection(c))
			continue
		}
		entry := scoreCandidate(track, expectedLength, c, maxViews)
		logCandidateEvaluated(ctx, track, entry)

		if c.Resolved {
			resolved = append(resolved, entry)
			continue
		}
		if entry.ident < identityMin && !entry.artistMatch {
			rejected = append(rejected, CandidateRejection{
				URL:    c.URL,
				Title:  c.Title,
				Source: c.Source,
				Stage:  RejectionIdentity,
				Reason: fmt.Sprintf("identity %.0f below threshold %.0f", entry.ident, identityMin),
			})
			continue
		}
		veto, fallback := UnrequestedQualifiers(track.Title, track.Artist, c.Title)
		if len(veto) > 0 {
			rejected = append(rejected, qualifierRejection(c, veto))
			continue
		}
		entry.isFallback = len(fallback) > 0
		if entry.uploader == channelTopic {
			topic = append(topic, entry)
		} else {
			other = append(other, entry)
		}
	}

	return resolved, topic, other, rejected
}
