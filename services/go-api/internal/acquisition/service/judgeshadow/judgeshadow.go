package judgeshadow

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/judge"
	"context"
	"log/slog"
)

var choose = judge.Choose

type verdict string

const (
	verdictAgree          verdict = "agree"
	verdictDisagree       verdict = "disagree"
	verdictJudgeAbstained verdict = "judge_abstained"
	verdictPipelineFailed verdict = "pipeline_failed"
)

func Log(ctx context.Context, target judge.Target, trackID string, kept *ports.AudioCandidate, keptProbedSeconds float64, candidates []ports.AudioCandidate) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "acquisition.judge_shadow_panic", "track_id", trackID, "panic", r)
		}
	}()
	keptURL := ""
	if kept != nil {
		keptURL = kept.URL
	}
	judgeURL, reason := "", "no candidate accepted"
	if best, decision, found := choose(target, observed(candidates, keptURL, keptProbedSeconds)); found {
		judgeURL, reason = best.URL, decision.Reason
	}
	slog.InfoContext(ctx, "acquisition.judge_shadow",
		"track_id", trackID,
		"pipeline_url", keptURL,
		"judge_url", judgeURL,
		"judge_reason", reason,
		"verdict", verdictOf(kept != nil, keptURL, judgeURL),
	)
}

func verdictOf(keptAny bool, keptURL, judgeURL string) verdict {
	switch {
	case !keptAny:
		return verdictPipelineFailed
	case judgeURL == "":
		return verdictJudgeAbstained
	case judgeURL == keptURL:
		return verdictAgree
	default:
		return verdictDisagree
	}
}

func observed(candidates []ports.AudioCandidate, keptURL string, keptProbedSeconds float64) []judge.Candidate {
	out := make([]judge.Candidate, 0, len(candidates))
	for _, c := range candidates {
		length := c.Duration
		if length == 0 && keptURL != "" && c.URL == keptURL {
			length = keptProbedSeconds
		}
		out = append(out, judge.Candidate{
			URL:      c.URL,
			Kind:     kindOf(c.Kind),
			Title:    c.Title,
			Artists:  c.Artists,
			Album:    c.Album,
			Length:   length,
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
