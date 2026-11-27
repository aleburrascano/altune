package judgeshadow

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/titlematch"
)

type Verdict string

const (
	VerdictAgree          Verdict = "agree"
	VerdictDisagree       Verdict = "disagree"
	VerdictJudgeAbstained Verdict = "judge_abstained"
	VerdictPipelineFailed Verdict = "pipeline_failed"
)

type Comparison struct {
	PipelineURL string
	JudgeURL    string
	Reason      string
	Verdict     Verdict
}

func Compare(target titlematch.Target, kept *ports.AudioCandidate, keptProbedSeconds float64, candidates []ports.AudioCandidate) Comparison {
	keptURL := ""
	if kept != nil {
		keptURL = kept.URL
	}
	judgeURL, reason := "", "no candidate accepted"
	if best, decision, found := titlematch.Choose(target, observed(candidates, keptURL, keptProbedSeconds)); found {
		judgeURL, reason = best.URL, decision.Reason
	}
	return Comparison{
		PipelineURL: keptURL,
		JudgeURL:    judgeURL,
		Reason:      reason,
		Verdict:     verdictOf(kept != nil, keptURL, judgeURL),
	}
}

func verdictOf(keptAny bool, keptURL, judgeURL string) Verdict {
	switch {
	case !keptAny:
		return VerdictPipelineFailed
	case judgeURL == "":
		return VerdictJudgeAbstained
	case judgeURL == keptURL:
		return VerdictAgree
	default:
		return VerdictDisagree
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
	case ports.KindUpload:
		return titlematch.Upload
	}
	return titlematch.Upload
}
