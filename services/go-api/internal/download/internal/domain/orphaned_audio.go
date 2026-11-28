package domain

import (
	"altune/go-api/internal/shared/ids"
	"time"
)

type OrphanedAudio struct {
	AudioRef  string
	UserId    ids.UserId
	TrackId   ids.TrackId
	NotBefore time.Time
}
