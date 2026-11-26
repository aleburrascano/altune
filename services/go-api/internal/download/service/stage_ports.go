package service

import "altune/go-api/internal/download/internal/core/ports"

type StagePorts struct {
	Prober      ports.AudioProber
	Tagger      ports.AudioTagger
	Rejections  ports.RejectionStore
	StoredAudio ports.StoredAudioReader
	Orphans     ports.OrphanedAudioRecorder
	Skips       ports.VerifySkipRecorder
}

func (p StagePorts) require(owner string) {
	requirePort(owner, "Prober", p.Prober == nil)
	requirePort(owner, "Tagger", p.Tagger == nil)
	requirePort(owner, "Rejections", p.Rejections == nil)
	requirePort(owner, "StoredAudio", p.StoredAudio == nil)
	requirePort(owner, "Orphans", p.Orphans == nil)
	requirePort(owner, "Skips", p.Skips == nil)
}
