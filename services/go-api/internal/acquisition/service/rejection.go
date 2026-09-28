package service

import (
	"fmt"
	"sort"
	"strings"
)

type RejectionStage string

const (
	RejectionIdentity     RejectionStage = "identity"
	RejectionDownload     RejectionStage = "download"
	RejectionDuration     RejectionStage = "duration"
	RejectionUndecodable  RejectionStage = "undecodable"
	RejectionFingerprint  RejectionStage = "fingerprint"
	RejectionNotAttempted RejectionStage = "not_attempted"
)

type CandidateRejection struct {
	URL    string
	Title  string
	Source string
	Stage  RejectionStage
	Reason string
}

func (ac *AcquisitionContext) recordRejection(url, title, source string, stage RejectionStage, reason string) {
	ac.Rejections = append(ac.Rejections, CandidateRejection{
		URL:    url,
		Title:  title,
		Source: source,
		Stage:  stage,
		Reason: reason,
	})
}

func summarizeRejections(rejections []CandidateRejection) string {
	if len(rejections) == 0 {
		return ""
	}

	byStage := make(map[string]int, len(rejections))
	for _, r := range rejections {
		byStage[string(r.Stage)]++
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
