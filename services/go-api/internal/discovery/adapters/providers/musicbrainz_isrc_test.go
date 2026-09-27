package providers

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestMusicBrainzAdapter_RecordingsByISRC(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ws/2/isrc/CAA509814003") {
			t.Errorf("path = %q, want the isrc lookup", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"isrc":"CAA509814003","recordings":[
			{"id":"5d6efd30-23c6-4a9b-85af-706ea2df9022","title":"Drinking in L.A.","length":236666},
			{"id":"no-length","title":"Drinking in L.A."}]}`))
	})

	got, err := a.RecordingsByISRC(context.Background(), " caa509814003 ")
	if err != nil {
		t.Fatalf("RecordingsByISRC: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d recordings, want 2: %+v", len(got), got)
	}
	if got[0].MBID != "5d6efd30-23c6-4a9b-85af-706ea2df9022" || got[0].Duration != 236 {
		t.Errorf("first = %+v, want the album recording at 236s", got[0])
	}
	if got[1].Duration != 0 {
		t.Errorf("a recording without a length must carry zero, got %d", got[1].Duration)
	}
}

func TestMusicBrainzAdapter_RecordingsByISRC_UnknownISRCIsEmpty(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	got, err := a.RecordingsByISRC(context.Background(), "XX0000000000")
	if err != nil || len(got) != 0 {
		t.Errorf("unknown ISRC = %+v, %v; want empty, nil", got, err)
	}
}

func TestMusicBrainzAdapter_RecordingsByISRC_ServerErrorIsError(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, err := a.RecordingsByISRC(context.Background(), "CAA509814003"); err == nil {
		t.Error("a 503 must surface as an error")
	}
}
