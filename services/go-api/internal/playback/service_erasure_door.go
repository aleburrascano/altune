package playback

import erasure "altune/go-api/internal/playback/service/erasure"

type ErasureSweeper = erasure.Sweeper

var (
	NewErasureSweeper       = erasure.NewSweeper
	WithErasedStateReaper   = erasure.WithErasedStateReaper
	WithErasureSweepMetrics = erasure.WithErasureSweepMetrics
	WithSweepRunBudget      = erasure.WithSweepRunBudget
)
