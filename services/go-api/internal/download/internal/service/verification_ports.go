package service

import (
	"altune/go-api/internal/download/internal/ports"
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
		missingPort(owner, "Identifier", v.Identifier),
		missingPort(owner, "RecordingLookup", v.RecordingLookup),
		missingPort(owner, "Rerecordings", v.Rerecordings),
		missingPort(owner, "Previews", v.Previews),
		missingPort(owner, "Recordings", v.Recordings),
	)
}
