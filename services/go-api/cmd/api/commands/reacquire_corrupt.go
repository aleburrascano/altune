package commands

import (
	"altune/go-api/internal/shared/config"
)

func RunReacquireCorruptM4a(cfg *config.Config, execute bool, limit int) {
	runReacquire(cfg, execute, limit, reacquireSpec{
		audioRefLikePattern:   "%.m4a",
		bannerFormatWithCount: "Found %d ready .m4a track(s) to re-acquire as MP3...",
		doneHeading:           "Re-acquire corrupt M4A complete:",
		successLabel:          "Fixed",
		orphanLogMsg:          "reacquire_corrupt: orphaned old m4a after db update",
	})
}
