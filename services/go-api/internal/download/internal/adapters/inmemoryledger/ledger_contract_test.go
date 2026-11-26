package inmemoryledger_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	"altune/go-api/internal/download/internal/core/ports"
	"testing"
)

type seededLedger struct{ *inmemoryledger.Ledger }

func (l seededLedger) AddTrack(_ *testing.T, track ports.TrackSnapshot) {
	l.Seed(inmemoryledger.LedgerTrack{TrackSnapshot: track})
}

func TestLedger_KeepsTheLedgerContract(t *testing.T) {
	acquisitiontest.LedgerContract(t, func(*testing.T) acquisitiontest.LedgerUnderTest {
		return seededLedger{inmemoryledger.NewLedger()}
	})
}
