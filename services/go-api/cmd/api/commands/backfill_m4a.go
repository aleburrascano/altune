package commands

import (
	"altune/go-api/internal/shared/config"
)

// RunBackfillM4a re-acquires every ready track whose stored audio is the legacy
// re-encoded MP3. Historical note: this originally converted MP3 → native M4A;
// after the ID3-on-m4a corruption the pipeline was reverted to MP3, so today it
// simply re-runs each track through the fully-gated pipeline (the new ref's
// extension follows whatever the pipeline now produces).
//
// Dry-run by default; pass execute=true to apply. limit<=0 processes all.
func RunBackfillM4a(cfg *config.Config, execute bool, limit int) {
	runReacquire(cfg, execute, limit, reacquireSpec{
		refLike:           "%.mp3",
		banner:            "Found %d ready MP3 track(s) to re-acquire...",
		doneHeading:       "Backfill complete:",
		okLabel:           "Converted",
		orphanLogMsg:      "backfill_m4a: orphaned old mp3 after db update",
		completedLogEvent: "backfill_m4a_completed",
	})
}
