package service_test

import (
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"testing"
)

func TestWire_TheStagesAJobPublishesAreTheFixtureTheClientMapsToPhases(t *testing.T) {
	job := scheduleAndFinish(t, freshAudioSource{})

	stages, err := json.Marshal(job.stages)
	if err != nil {
		t.Fatal(err)
	}

	jsoncontract.AssertWireExact(t, "download_stages", stages)
}
