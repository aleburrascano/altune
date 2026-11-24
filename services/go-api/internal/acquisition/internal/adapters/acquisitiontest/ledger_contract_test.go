package acquisitiontest_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"testing"
)

type seededLedger struct{ *acquisitiontest.Ledger }

func (l seededLedger) AddTrack(_ *testing.T, track ports.TrackSnapshot) {
	l.Seed(acquisitiontest.LedgerTrack{TrackSnapshot: track})
}

func TestLedger_KeepsTheLedgerContract(t *testing.T) {
	acquisitiontest.LedgerContract(t, func(*testing.T) acquisitiontest.LedgerUnderTest {
		return seededLedger{acquisitiontest.NewLedger()}
	})
}
