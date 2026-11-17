package playback

import erasure "altune/go-api/internal/playback/internal/core/erasure"

type ErasureSweeper = erasure.Sweeper

var (
	NewErasureSweeper       = erasure.NewSweeper
	WithErasedStateReaper   = erasure.WithErasedStateReaper
	WithErasureSweepMetrics = erasure.WithErasureSweepMetrics
	WithSweepRunBudget      = erasure.WithSweepRunBudget
)
