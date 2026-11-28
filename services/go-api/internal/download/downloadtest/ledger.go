package downloadtest

import "altune/go-api/internal/download/internal/adapters/memory"

type (
	Ledger      = memory.Ledger
	LedgerTrack = memory.LedgerTrack
)

var NewLedger = memory.NewLedger
