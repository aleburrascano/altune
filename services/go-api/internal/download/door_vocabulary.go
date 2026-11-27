package download

import (
	ports "altune/go-api/internal/download/internal/core/ports"
)

type ClaimNumber = ports.ClaimNumber

type EvidenceFate = ports.EvidenceFate

const (
	KeepEvidence  = ports.KeepEvidence
	ResetEvidence = ports.ResetEvidence
)
