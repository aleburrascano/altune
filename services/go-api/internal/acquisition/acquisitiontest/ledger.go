package acquisitiontest

import fake "altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"

type (
	Ledger      = fake.Ledger
	LedgerTrack = fake.LedgerTrack
)

var NewLedger = fake.NewLedger
