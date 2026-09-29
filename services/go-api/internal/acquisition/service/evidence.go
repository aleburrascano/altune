package service

type Evidence struct {
	Verdict              string   `json:"verdict"`
	AcoustIDScore        float64  `json:"acoustid_score"`
	ISRCSetHit           bool     `json:"isrc_set_hit"`
	DurationDeltaSeconds float64  `json:"duration_delta_seconds"`
	Channel              string   `json:"channel"`
	Qualifiers           []string `json:"qualifiers"`
	Resolved             bool     `json:"resolved"`
	SourceTitle          string   `json:"source_title"`
}

func (ac *AcquisitionContext) collectEvidence() Evidence {
	evidence := ac.Evidence
	evidence.Verdict = string(ac.Verdict.Kind)
	evidence.AcoustIDScore = ac.Verdict.Score
	if ac.Selected != nil {
		evidence.SourceTitle = ac.Selected.Title
	}
	return evidence
}
