package download

import (
	"altune/go-api/internal/download/internal/ports"
)

type ClaimNumber = ports.ClaimNumber

type EvidenceFate = ports.EvidenceFate

const (
	KeepEvidence  = ports.KeepEvidence
	ResetEvidence = ports.ResetEvidence
)
