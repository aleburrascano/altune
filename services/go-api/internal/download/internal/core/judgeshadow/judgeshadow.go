package judgeshadow

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/titlematch"
	"altune/go-api/internal/shared/background"
	"context"
	"log/slog"
)

type verdict string

const (
	verdictAgree          verdict = "agree"
	verdictDisagree       verdict = "disagree"
	verdictJudgeAbstained verdict = "judge_abstained"
	verdictPipelineFailed verdict = "pipeline_failed"
)

func Log(ctx context.Context, target titlematch.Target, trackID string, kept *ports.AudioCandidate, keptProbedSeconds float64, candidates []ports.AudioCandidate) {
	defer background.RecoverContext(ctx, "acquisition.judge_shadow_panic", "track_id", trackID)
	keptURL := ""
	if kept != nil {
		keptURL = kept.URL
	}
	judgeURL, reason := "", "no candidate accepted"
	if best, decision, found := titlematch.Choose(target, observed(candidates, keptURL, keptProbedSeconds)); found {
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

func observed(candidates []ports.AudioCandidate, keptURL string, keptProbedSeconds float64) []titlematch.Candidate {
	out := make([]titlematch.Candidate, 0, len(candidates))
	for _, c := range candidates {
		length := c.Duration
		if length == 0 && keptURL != "" && c.URL == keptURL {
			length = keptProbedSeconds
		}
		out = append(out, titlematch.Candidate{
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

func kindOf(k ports.CandidateKind) titlematch.Kind {
	switch k {
	case ports.KindArtTrack:
		return titlematch.ArtTrack
	case ports.KindPinnedVideo, ports.KindCatalogPin:
		return titlematch.PinnedVideo
	default:
		return titlematch.Upload
	}
}
