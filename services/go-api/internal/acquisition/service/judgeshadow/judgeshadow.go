package judgeshadow

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/judge"
	"context"
	"log/slog"
)

var choose = judge.Choose

func Log(ctx context.Context, target judge.Target, trackID string, pipelinePick ports.AudioCandidate, candidates []ports.AudioCandidate) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "acquisition.judge_shadow_panic", "track_id", trackID, "panic", r)
		}
	}()
	judgeURL, reason := "", "no candidate accepted"
	if best, decision, found := choose(target, observed(candidates)); found {
		judgeURL, reason = best.URL, decision.Reason
	}
	slog.InfoContext(ctx, "acquisition.judge_shadow",
		"track_id", trackID,
		"pipeline_url", pipelinePick.URL,
		"judge_url", judgeURL,
		"judge_reason", reason,
		"agree", judgeURL == pipelinePick.URL,
	)
}

func observed(candidates []ports.AudioCandidate) []judge.Candidate {
	out := make([]judge.Candidate, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, judge.Candidate{
			URL:      c.URL,
			Kind:     kindOf(c.Kind),
			Title:    c.Title,
			Artists:  c.Artists,
			Album:    c.Album,
			Length:   c.Duration,
			Explicit: c.Explicit,
		})
	}
	return out
}

func kindOf(k ports.CandidateKind) judge.Kind {
	switch k {
	case ports.KindArtTrack:
		return judge.ArtTrack
	case ports.KindPinnedVideo, ports.KindCatalogPin:
		return judge.PinnedVideo
	default:
		return judge.Upload
	}
}
