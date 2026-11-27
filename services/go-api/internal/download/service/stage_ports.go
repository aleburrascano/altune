package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/random"
	"errors"
)

type StagePorts struct {
	Prober        ports.AudioProber
	Tagger        ports.AudioTagger
	Rejections    ports.RejectionStore
	StoredAudio   ports.StoredAudioReader
	Orphans       ports.OrphanedAudioRecorder
	Skips         ports.VerifySkipRecorder
	Events        events.Publisher
	DownloadLimit *ConcurrencyLimit
	Observer      func(*AcquisitionContext)
	Sleep         clock.Sleeper
	Now           clock.Func
	Unit          random.Unit
}

func NoObserver() func(*AcquisitionContext) { return func(*AcquisitionContext) {} }

func (p StagePorts) validate(owner string) error {
	return errors.Join(
		missingPort(owner, "Prober", p.Prober),
		missingPort(owner, "Tagger", p.Tagger),
		missingPort(owner, "Rejections", p.Rejections),
		missingPort(owner, "StoredAudio", p.StoredAudio),
		missingPort(owner, "Orphans", p.Orphans),
		missingPort(owner, "Skips", p.Skips),
		missingPort(owner, "Events", p.Events),
		missingPort(owner, "DownloadLimit", p.DownloadLimit),
		missingPort(owner, "Observer", p.Observer),
		missingPort(owner, "Sleep", p.Sleep),
		missingPort(owner, "Now", p.Now),
		missingPort(owner, "Unit", p.Unit),
	)
}
