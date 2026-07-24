package commands

import (
	"altune/go-api/internal/shared/config"
)

func RunBackfillM4a(cfg *config.Config, execute bool, limit int) {
	runReacquire(cfg, execute, limit, reacquireSpec{
		audioRefLikePattern:     "%.mp3",
		bannerFormatWithCount:   "Found %d ready MP3 track(s) to re-acquire...",
		doneHeading:             "Backfill complete:",
		successLabel:            "Converted",
		orphanLogMsg:            "backfill_m4a: orphaned old mp3 after db update",
		completedLogEventOrNone: "backfill_m4a_completed",
	})
}
