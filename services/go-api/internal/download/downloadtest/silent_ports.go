package downloadtest

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
)

type silentAudioProber struct{}

func SilentAudioProber() ports.AudioProber { return silentAudioProber{} }

func (silentAudioProber) ProbeDuration(context.Context, string) (float64, error) {
	return 0, ports.ErrNoAudioProber
}

func (silentAudioProber) ValidateDecodable(context.Context, string) error { return nil }

type silentAudioRefLookup struct{}

func SilentAudioRefLookup() ports.AudioRefLookup { return silentAudioRefLookup{} }

func (silentAudioRefLookup) AudioRefInUse(context.Context, string, ids.TrackId) (bool, error) {
	return false, nil
}

type silentRecordingSearch struct{}

func SilentRecordingSearch() ports.RecordingSearch { return silentRecordingSearch{} }

func (silentRecordingSearch) SearchRecordings(context.Context, string, int) ([]ports.SearchedRecording, error) {
	return nil, nil
}
