package service

import (
	"fmt"
	"sort"
	"strings"
)

// CandidateRejection records why a single candidate was discarded during
// acquisition. It exists so a failure is explainable after the fact: the
// reasons are folded into the persisted failure_reason rather than living only
// in ephemeral logs.
type CandidateRejection struct {
	URL    string
	Title  string
	Source string
	Stage  string
	Reason string
}

// recordRejection appends a per-candidate rejection to the acquisition context.
func (ac *AcquisitionContext) recordRejection(url, title, source, stage, reason string) {
	ac.Rejections = append(ac.Rejections, CandidateRejection{
		URL:    url,
		Title:  title,
		Source: source,
		Stage:  stage,
		Reason: reason,
	})
}

// summarizeRejections renders a compact, deterministic breakdown of why every
// candidate was rejected, safe to persist (it carries counts and stage names,
// never raw tool output). It returns "" when nothing was rejected.
func summarizeRejections(rejections []CandidateRejection) string {
	if len(rejections) == 0 {
		return ""
	}

	byStage := make(map[string]int, len(rejections))
	for _, r := range rejections {
		byStage[r.Stage]++
	}

	stages := make([]string, 0, len(byStage))
	for stage := range byStage {
		stages = append(stages, stage)
	}
	sort.Strings(stages)

	parts := make([]string, 0, len(stages))
	for _, stage := range stages {
		parts = append(parts, fmt.Sprintf("%d %s", byStage[stage], stage))
	}

	noun := "candidates"
	if len(rejections) == 1 {
		noun = "candidate"
	}
	return fmt.Sprintf("all %d %s rejected (%s)", len(rejections), noun, strings.Join(parts, ", "))
}
