package playback

import erasure "altune/go-api/internal/playback/internal/core/deleteduserdata"

type DeletedUserDataCleanup = erasure.Sweeper

const DefaultSweepRunBudget = erasure.DefaultSweepRunBudget

var NewErasureSweeper = erasure.NewSweeper
