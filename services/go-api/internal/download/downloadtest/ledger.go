package downloadtest

import fake "altune/go-api/internal/download/internal/adapters/inmemoryledger"

type (
	Ledger      = fake.Ledger
	LedgerTrack = fake.LedgerTrack
)

var NewLedger = fake.NewLedger
