package downloadtest

import "altune/go-api/internal/download/internal/adapters/inmemoryledger"

type (
	Ledger      = inmemoryledger.Ledger
	LedgerTrack = inmemoryledger.LedgerTrack
)

var NewLedger = inmemoryledger.NewLedger
