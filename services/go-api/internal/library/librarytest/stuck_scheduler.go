package librarytest

import (
	"altune/go-api/internal/library/internal/adapters/memory"
)

type StuckScheduler = memory.StuckScheduler

var AssertScheduleDeadline = memory.AssertScheduleDeadline
