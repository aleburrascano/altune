package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	"net/http"
	"strings"
	"testing"
)

// An internal-looking source_url must be refused at the HTTP boundary before a
// track is stored or the acquisition scheduler is asked to fetch it.
func TestHandleCreateTrack_RejectsInternalSourceURL(t *testing.T) {
	for _, sourceURL := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:8080/admin",
		"http://[::1]/",
		"http://10.0.0.1/",
	} {
		t.Run(sourceURL, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			sched := &catalogtest.Scheduler{}
			_, router := buildTrackHandler(repo, sched)
			body := CreateTrackRequest{Title: "Dreams", Artist: "Fleetwood Mac", SourceURL: strPtr(sourceURL)}

			rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

			assertStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), "public host") {
				t.Errorf("body = %s, want the public-host validation message", rec.Body.String())
			}
			if len(sched.SourceURLs) != 0 || len(repo.Tracks) != 0 {
				t.Errorf("scheduled %v and stored %d tracks, want neither", sched.SourceURLs, len(repo.Tracks))
			}
		})
	}
}

func TestHandleCreateTrack_SchedulesPublicSourceURL(t *testing.T) {
	sched := &catalogtest.Scheduler{}
	_, router := buildTrackHandler(catalogtest.NewTrackRepo(), sched)
	sourceURL := "https://soundcloud.com/fleetwoodmac/dreams"
	body := CreateTrackRequest{Title: "Dreams", Artist: "Fleetwood Mac", SourceURL: strPtr(sourceURL)}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))

	assertStatus(t, rec, http.StatusCreated)
	if len(sched.SourceURLs) != 1 || sched.SourceURLs[0] != sourceURL {
		t.Errorf("scheduler got %v, want [%s]", sched.SourceURLs, sourceURL)
	}
}
