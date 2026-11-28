package outboundtest

import (
	"altune/go-api/internal/platform/outbound/internal/adapters/egress"
	"altune/go-api/internal/platform/outbound/internal/adapters/youtubemusic"
)

var (
	ErrNonPublicAddr             = egress.ErrNonPublicAddr
	ErrYouTubeMusicBudgetRefused = youtubemusic.ErrBudgetRefused
)
