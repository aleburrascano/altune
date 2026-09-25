package handler

import (
	"net/http"
	"strings"
	"testing"
)

func TestSubmitReport_RejectsOversizeBodyWithoutCreating(t *testing.T) {
	tracker := &stubTracker{}
	body := `{"kind":"bug","message":"grey tracks","app_version":"` + strings.Repeat("a", 1<<20) + `"}`
	rec := send(t, router(tracker), strings.NewReader(body), true)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 400 or 413", rec.Code)
	}
	if tracker.creates != 0 {
		t.Fatalf("creates = %d, want 0", tracker.creates)
	}
}

func TestSubmitReport_AcceptsMaximumSizeReport(t *testing.T) {
	tracker := &stubTracker{}
	body := validBody()
	body["message"] = strings.Repeat("é", 2000)
	for _, k := range []string{"app_version", "platform", "os_version", "screen"} {
		body[k] = strings.Repeat("é", 64)
	}
	assertStatus(t, post(t, router(tracker), body), http.StatusCreated)
}
