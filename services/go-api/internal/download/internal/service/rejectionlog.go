package service

import (
	"altune/go-api/internal/download/internal/domain"
	"fmt"
	"sort"
	"strings"
)

type RejectionStage string

const (
	RejectIdentity     RejectionStage = RejectionStage(StageIdentity)
	RejectQualifier    RejectionStage = RejectionStage(StageQualifier)
	RejectDownload     RejectionStage = "download"
	RejectDRM          RejectionStage = RejectionStage(StageDRM)
	RejectPreview      RejectionStage = RejectionStage(StagePreview)
	RejectDuration     RejectionStage = "duration"
	RejectUndecodable  RejectionStage = "undecodable"
	RejectFingerprint  RejectionStage = "fingerprint"
	RejectNotAttempted RejectionStage = "not_attempted"
	RejectCurrentFile  RejectionStage = "current_file"
)

type RejectionEntry struct {
	URL        string
	Title      string
	Source     domain.SourceName
	Stage      RejectionStage
	Reason     string
	Unmeasured bool
}

func SummarizeRejections(entries []RejectionEntry) string {
	if len(entries) == 0 {
		return ""
	}
	byStage, rejected, notTried := tally(finalStages(entries))
	if rejected == 0 {
		return fmt.Sprintf("%d %s not tried", notTried, candidateNoun(notTried))
	}
	summary := fmt.Sprintf("all %d %s rejected (%s)", rejected, candidateNoun(rejected), stageCounts(byStage))
	if notTried > 0 {
		summary += fmt.Sprintf(", %d not tried", notTried)
	}
	return summary
}

func finalStages(entries []RejectionEntry) map[string]RejectionStage {
	final := make(map[string]RejectionStage, len(entries))
	for i, e := range entries {
		key := candidateKey(i, e)
		_, seen := final[key]
		switch {
		case e.Stage != RejectNotAttempted:
			final[key] = e.Stage
		case !seen:
			final[key] = ""
		}
	}
	return final
}

func candidateKey(i int, e RejectionEntry) string {
	if key := domain.SourceKey(e.URL); key != "" {
		return key
	}
	if e.URL != "" {
		return e.URL
	}
	return fmt.Sprintf("#%d", i)
}

func tally(final map[string]RejectionStage) (byStage map[string]int, rejected, notTried int) {
	byStage = make(map[string]int, len(final))
	for _, stage := range final {
		if stage == "" {
			notTried++
			continue
		}
		byStage[string(stage)]++
		rejected++
	}
	return byStage, rejected, notTried
}

func stageCounts(byStage map[string]int) string {
	stages := make([]string, 0, len(byStage))
	for stage := range byStage {
		stages = append(stages, stage)
	}
	sort.Strings(stages)
	parts := make([]string, 0, len(stages))
	for _, stage := range stages {
		parts = append(parts, fmt.Sprintf("%d %s", byStage[stage], stage))
	}
	return strings.Join(parts, ", ")
}

func candidateNoun(n int) string {
	if n == 1 {
		return "candidate"
	}
	return "candidates"
}

func (s RejectionStage) lasting() bool {
	return s != RejectDownload && s != RejectNotAttempted && s != RejectCurrentFile
}

func LastingRejections(entries []RejectionEntry, trackID string, basis domain.RejectionBasis) []domain.CandidateRejectionRecord {
	recs := make([]domain.CandidateRejectionRecord, 0, len(entries))
	for _, e := range entries {
		key := domain.SourceKey(e.URL)
		if !e.Stage.lasting() || e.Unmeasured || key == "" {
			continue
		}
		recs = append(recs, domain.CandidateRejectionRecord{
			TrackID:   trackID,
			SourceKey: key,
			Reason:    string(e.Stage),
			Detail:    e.Reason,
			Basis:     basis,
		})
	}
	return recs
}
