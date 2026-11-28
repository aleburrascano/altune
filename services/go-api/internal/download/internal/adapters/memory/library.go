package memory

import (
	"altune/go-api/internal/download/internal/ports"
	"encoding/json"
)

type Library struct {
	*Ledger
}

var _ ports.TrackLibrary = (*Library)(nil)

func NewLibrary(tracks ...LedgerTrack) *Library {
	return &Library{Ledger: NewLedger(tracks...)}
}

func (*Library) EvidenceAccepted(c ports.Completion) bool {
	return c.Evidence != nil && json.Valid(c.Evidence) && c.Confidence >= 0 && c.Confidence <= 1
}
