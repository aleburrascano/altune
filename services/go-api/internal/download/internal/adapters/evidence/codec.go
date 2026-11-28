package evidence

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"encoding/json"
	"fmt"
)

type Codec struct{}

var _ ports.EvidenceCodec = Codec{}

func NewCodec() Codec { return Codec{} }

type acquisitionRecord struct {
	Verdict              string             `json:"verdict"`
	AcoustIDScore        float64            `json:"acoustid_score"`
	DurationDeltaSeconds *float64           `json:"duration_delta_seconds"`
	Channel              string             `json:"channel"`
	Qualifiers           []string           `json:"qualifiers"`
	Resolved             bool               `json:"resolved"`
	SourceTitle          string             `json:"source_title"`
	ISRCSource           domain.ProofSource `json:"isrc_source"`
	SourceKind           domain.SourceKind  `json:"source_kind"`
	PipelineGeneration   int                `json:"pipeline_generation"`
	Grade                domain.Grade       `json:"grade"`
	SourceFormatID       string             `json:"source_format_id,omitempty"`
	SourceKbps           int                `json:"source_kbps,omitempty"`
}

type inPlaceRecord struct {
	Grade             domain.Grade      `json:"grade"`
	SourceKind        domain.SourceKind `json:"source_kind"`
	InPlaceGeneration int               `json:"in_place_generation"`
	SourceFormatID    string            `json:"source_format_id"`
	SourceKbps        int               `json:"source_kbps"`
}

func (Codec) Encode(evidence domain.AcquisitionEvidence) (json.RawMessage, error) {
	raw, err := json.Marshal(acquisitionRecord(evidence))
	if err != nil {
		return nil, fmt.Errorf("marshal acquisition evidence: %w", err)
	}
	return raw, nil
}

func (Codec) Decode(raw json.RawMessage) (domain.AcquisitionEvidence, error) {
	var stored acquisitionRecord
	if err := json.Unmarshal(raw, &stored); err != nil {
		return domain.AcquisitionEvidence{}, fmt.Errorf("read acquisition evidence: %w", err)
	}
	return domain.AcquisitionEvidence(stored), nil
}

func (Codec) DecodeInPlace(raw json.RawMessage) (domain.InPlaceEvidence, error) {
	var stored inPlaceRecord
	if err := json.Unmarshal(raw, &stored); err != nil {
		return domain.InPlaceEvidence(stored), fmt.Errorf("read in-place evidence: %w", err)
	}
	return domain.InPlaceEvidence(stored), nil
}
