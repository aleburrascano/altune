package handler_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"net/http"
	"testing"
)

func TestWire_TheAcquisitionRoutesAnswerAcceptedWithoutABodyTheClientReads(t *testing.T) {
	retryRepo := memory.NewLedger()
	retryTrack := failedTrack(retryTestUserId)
	retryRepo.Seed(retryTrack)
	reacquireRepo := memory.NewLedger()
	reacquireTrack := readyTrack(reacquireTestUserId)
	reacquireRepo.Seed(reacquireTrack)

	retry := retryServe(t, buildRetryRouter(retryRepo, &retryFakeScheduler{}), http.MethodPost, "/tracks/"+retryTrack.ID.UUID().String()+"/retry")
	reacquire := retryServe(t, buildReacquireRouter(reacquireRepo, &reacquireFakeScheduler{}), http.MethodPost, "/tracks/"+reacquireTrack.ID.UUID().String()+"/reacquire")

	body, err := json.Marshal(map[string]int{
		"POST /v1/tracks/{}/retry":     retry.Code,
		"POST /v1/tracks/{}/reacquire": reacquire.Code,
	})
	if err != nil {
		t.Fatalf("marshal statuses: %v", err)
	}
	jsoncontract.AssertWireExact(t, "acquisition_routes", body)
}
