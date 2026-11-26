package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
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
}

func NoObserver() func(*AcquisitionContext) { return func(*AcquisitionContext) {} }

func (p StagePorts) validate(owner string) error {
	return errors.Join(
		missingPort(owner, "Prober", p.Prober == nil),
		missingPort(owner, "Tagger", p.Tagger == nil),
		missingPort(owner, "Rejections", p.Rejections == nil),
		missingPort(owner, "StoredAudio", p.StoredAudio == nil),
		missingPort(owner, "Orphans", p.Orphans == nil),
		missingPort(owner, "Skips", p.Skips == nil),
		missingPort(owner, "Events", p.Events == nil),
		missingPort(owner, "DownloadLimit", p.DownloadLimit == nil),
		missingPort(owner, "Observer", p.Observer == nil),
		missingPort(owner, "Sleep", p.Sleep == nil),
		missingPort(owner, "Now", p.Now == nil),
	)
}
