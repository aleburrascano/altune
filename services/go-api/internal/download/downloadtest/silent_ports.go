package downloadtest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/songfields"
	"context"
)

type silentAudioProber struct{}

func SilentAudioProber() ports.AudioProber { return silentAudioProber{} }

func (silentAudioProber) ProbeDuration(context.Context, string) (float64, error) {
	return 0, domain.ErrNoAudioProber
}

func (silentAudioProber) ValidateDecodable(context.Context, string) error { return nil }

type silentAudioRefLookup struct{}

func SilentAudioRefLookup() ports.AudioRefLookup { return silentAudioRefLookup{} }

func (silentAudioRefLookup) AudioRefInUse(context.Context, string, ids.TrackId) (bool, error) {
	return false, nil
}

type silentRecordingSearch struct{}

func SilentRecordingSearch() ports.RecordingSearch { return silentRecordingSearch{} }

func (silentRecordingSearch) SearchRecordings(context.Context, string, int) ([]songfields.RecordingIdentity, error) {
	return nil, nil
}
