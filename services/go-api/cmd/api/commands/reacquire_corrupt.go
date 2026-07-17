package commands

import (
	"altune/go-api/internal/shared/config"
)

// RunReacquireCorruptM4a re-acquires every ready track whose stored audio is a
// .m4a — the corrupt files the ID3-on-m4a pipeline produced before it was
// reverted to MP3 — replacing each through the now-gated acquisition pipeline
// (search → select → download → decode-gate → tag → store-gate). A failed or
// gate-rejected re-acquire leaves the (already-broken) original in place rather
// than losing the row.
//
// Dry-run by default; execute=true applies. limit<=0 processes all.
func RunReacquireCorruptM4a(cfg *config.Config, execute bool, limit int) {
	runReacquire(cfg, execute, limit, reacquireSpec{
		refLike:      "%.m4a",
		banner:       "Found %d ready .m4a track(s) to re-acquire as MP3...",
		doneHeading:  "Re-acquire corrupt M4A complete:",
		okLabel:      "Fixed",
		orphanLogMsg: "reacquire_corrupt: orphaned old m4a after db update",
	})
}
