package service

import "altune/go-api/internal/download/internal/core/ports"

type VerificationPorts struct {
	Identifier      ports.AudioIdentifier
	RecordingLookup ports.RecordingLookup
	Rerecordings    ports.RerecordingFinder
	Previews        ports.PreviewMatcher
	Recordings      ports.RecordingResolver
}

func (v VerificationPorts) require(owner string) {
	requirePort(owner, "Identifier", v.Identifier == nil)
	requirePort(owner, "RecordingLookup", v.RecordingLookup == nil)
	requirePort(owner, "Rerecordings", v.Rerecordings == nil)
	requirePort(owner, "Previews", v.Previews == nil)
	requirePort(owner, "Recordings", v.Recordings == nil)
}

func requirePort(owner, name string, missing bool) {
	if missing {
		panic(owner + ": " + name + " is required; pass the explicit Noop when the wiring has none")
	}
}
