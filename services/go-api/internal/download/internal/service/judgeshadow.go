package service

import (
	"altune/go-api/internal/download/internal/domain"
)

type ShadowVerdict string

const (
	VerdictAgree          ShadowVerdict = "agree"
	VerdictDisagree       ShadowVerdict = "disagree"
	VerdictJudgeAbstained ShadowVerdict = "judge_abstained"
	VerdictPipelineFailed ShadowVerdict = "pipeline_failed"
)

type ShadowComparison struct {
	PipelineURL string
	JudgeURL    string
	Reason      string
	Verdict     ShadowVerdict
}

func CompareShadow(target TitleTarget, kept *domain.AudioCandidate, keptProbedSeconds float64, candidates []domain.AudioCandidate) ShadowComparison {
	keptURL := ""
	if kept != nil {
		keptURL = kept.URL
	}
	judgeURL, reason := "", "no candidate accepted"
	if best, decision, found := ChooseTitle(target, observed(candidates, keptURL, keptProbedSeconds)); found {
		judgeURL, reason = best.URL, decision.Reason
	}
	return ShadowComparison{
		PipelineURL: keptURL,
		JudgeURL:    judgeURL,
		Reason:      reason,
		Verdict:     verdictOf(kept != nil, keptURL, judgeURL),
	}
}

func verdictOf(keptAny bool, keptURL, judgeURL string) ShadowVerdict {
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

func observed(candidates []domain.AudioCandidate, keptURL string, keptProbedSeconds float64) []TitleCandidate {
	out := make([]TitleCandidate, 0, len(candidates))
	for _, c := range candidates {
		length := c.Duration
		if length == 0 && keptURL != "" && c.URL == keptURL {
			length = keptProbedSeconds
		}
		out = append(out, TitleCandidate{
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

func kindOf(k domain.CandidateKind) TitleKind {
	switch k {
	case domain.KindArtTrack:
		return TitleArtTrack
	case domain.KindPinnedVideo, domain.KindCatalogPin:
		return TitlePinnedVideo
	case domain.KindUpload:
		return TitleUpload
	}
	return TitleUpload
}
