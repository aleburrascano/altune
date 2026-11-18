package catalogtest

import memory "altune/go-api/internal/catalog/internal/adapters/memory"

type StuckScheduler = memory.StuckScheduler

var AssertScheduleDeadline = memory.AssertScheduleDeadline
