package service

import (
	"altune/go-api/internal/acquisition/ports"
	"testing"
)

func TestRecordingIdentity_SourceFor(t *testing.T) {
	id := ports.RecordingIdentity{Sources: []ports.RecordingSource{
		{Provider: "deezer", ExternalID: "123"},
		{Provider: "youtube", ExternalID: "abc"},
	}}

	if got, ok := id.SourceFor("youtube"); !ok || got.ExternalID != "abc" {
		t.Errorf("SourceFor(youtube) = %+v %v", got, ok)
	}
	if _, ok := id.SourceFor("tidal"); ok {
		t.Error("SourceFor(tidal) should report missing")
	}
}
