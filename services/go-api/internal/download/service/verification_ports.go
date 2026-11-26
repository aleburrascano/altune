package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"errors"
)

type VerificationPorts struct {
	Identifier      ports.AudioIdentifier
	RecordingLookup ports.RecordingLookup
	Rerecordings    ports.RerecordingFinder
	Previews        ports.PreviewMatcher
	Recordings      ports.RecordingResolver
}

func (v VerificationPorts) validate(owner string) error {
	return errors.Join(
		missingPort(owner, "Identifier", v.Identifier == nil),
		missingPort(owner, "RecordingLookup", v.RecordingLookup == nil),
		missingPort(owner, "Rerecordings", v.Rerecordings == nil),
		missingPort(owner, "Previews", v.Previews == nil),
		missingPort(owner, "Recordings", v.Recordings == nil),
	)
}
