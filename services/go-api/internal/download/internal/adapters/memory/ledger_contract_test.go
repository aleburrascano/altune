package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/ports"
	"testing"
)

type seededLedger struct{ *memory.Ledger }

func (l seededLedger) AddTrack(_ *testing.T, track ports.TrackSnapshot) {
	l.Seed(memory.LedgerTrack{TrackSnapshot: track})
}

func TestLedger_KeepsTheLedgerContract(t *testing.T) {
	contracttest.LedgerContract(t, func(*testing.T) contracttest.LedgerUnderTest {
		return seededLedger{memory.NewLedger()}
	})
}
