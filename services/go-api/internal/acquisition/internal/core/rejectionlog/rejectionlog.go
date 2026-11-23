package rejectionlog

import (
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/ranking"
	"altune/go-api/internal/acquisition/sourcekey"
	"fmt"
	"sort"
	"strings"
)

type Stage string

const (
	Identity     Stage = Stage(ranking.StageIdentity)
	Qualifier    Stage = Stage(ranking.StageQualifier)
	Download     Stage = "download"
	DRM          Stage = Stage(ranking.StageDRM)
	Preview      Stage = Stage(ranking.StagePreview)
	Duration     Stage = "duration"
	Undecodable  Stage = "undecodable"
	Fingerprint  Stage = "fingerprint"
	NotAttempted Stage = "not_attempted"
	CurrentFile  Stage = "current_file"
)

type Entry struct {
	URL        string
	Title      string
	Source     acqdomain.SourceName
	Stage      Stage
	Reason     string
	Unmeasured bool
}

func Summary(entries []Entry) string {
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

func finalStages(entries []Entry) map[string]Stage {
	final := make(map[string]Stage, len(entries))
	for i, e := range entries {
		key := candidateKey(i, e)
		if e.Stage != NotAttempted {
			final[key] = e.Stage
		} else if _, ok := final[key]; !ok {
			final[key] = ""
		}
	}
	return final
}

func candidateKey(i int, e Entry) string {
	if key := sourcekey.SourceKey(e.URL); key != "" {
		return key
	}
	if e.URL != "" {
		return e.URL
	}
	return fmt.Sprintf("#%d", i)
}

func tally(final map[string]Stage) (byStage map[string]int, rejected, notTried int) {
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

func (s Stage) lasting() bool {
	return s != Download && s != NotAttempted && s != CurrentFile
}

func Lasting(entries []Entry, trackID string, basis ports.RejectionBasis) []ports.CandidateRejectionRecord {
	recs := make([]ports.CandidateRejectionRecord, 0, len(entries))
	for _, e := range entries {
		key := sourcekey.SourceKey(e.URL)
		if !e.Stage.lasting() || e.Unmeasured || key == "" {
			continue
		}
		recs = append(recs, ports.CandidateRejectionRecord{
			TrackID:   trackID,
			SourceKey: key,
			Reason:    string(e.Stage),
			Detail:    e.Reason,
			Basis:     basis,
		})
	}
	return recs
}
