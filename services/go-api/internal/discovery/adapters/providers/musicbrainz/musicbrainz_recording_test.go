package musicbrainz

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"net/http"
	"reflect"
	"testing"
)

const (
	bohemianMBID    = "b1a9c0e9-d987-4042-ae91-78d6a3267d69"
	cureLiveMBID    = "95ccd64c-5b38-43e0-827d-bb14b271372a"
	bohemianLookup  = `{"title":"Bohemian Rhapsody","isrcs":["GBCEE0100112","GBCEE0500364","GBUM71029604"],"disambiguation":"","artist-credit":[{"name":"Queen","joinphrase":"","artist":{"id":"0383dadf-2a4e-4d10-a46a-e9e041da8eb3","name":"Queen"}}],"video":false,"id":"b1a9c0e9-d987-4042-ae91-78d6a3267d69","length":355106}`
	cureLiveLookup  = `{"disambiguation":"live, 1991-03-03: The Limehouse, London, UK","artist-credit":[{"artist":{"id":"69ee3720-a7cb-4402-b48d-a02c366f2bcf","name":"The Cure"},"joinphrase":"","name":"The Cure"}],"isrcs":[],"title":"Boys Don’t Cry","video":false,"length":187000,"id":"95ccd64c-5b38-43e0-827d-bb14b271372a"}`
	recordingLookup = "/ws/2/recording/"
)

func recordingServer(t *testing.T, bodies map[string]string) *MusicBrainzAdapter {
	t.Helper()
	return mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, known := bodies[r.URL.Path]
		if !known || r.URL.Query().Get("inc") != "isrcs artist-credits" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
}

func TestMusicBrainzAdapter_RecordingByMBID(t *testing.T) {
	a := recordingServer(t, map[string]string{
		recordingLookup + bohemianMBID: bohemianLookup,
		recordingLookup + cureLiveMBID: cureLiveLookup,
	})

	tests := []struct {
		name string
		mbid string
		want ports.MBRecording
	}{
		{
			name: "isrcs, length and artists of a studio recording",
			mbid: bohemianMBID,
			want: ports.MBRecording{
				MBID: bohemianMBID, Title: "Bohemian Rhapsody", Artists: []string{"Queen"},
				ISRCs:    []string{"GBCEE0100112", "GBCEE0500364", "GBUM71029604"},
				Duration: 355.106,
			},
		},
		{
			name: "disambiguation of a live recording",
			mbid: " " + cureLiveMBID + " ",
			want: ports.MBRecording{
				MBID: cureLiveMBID, Title: "Boys Don’t Cry", Artists: []string{"The Cure"},
				Disambiguation: "live, 1991-03-03: The Limehouse, London, UK",
				ISRCs:          []string{},
				Duration:       187,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := a.RecordingByMBID(context.Background(), tt.mbid)
			if err != nil || !found {
				t.Fatalf("RecordingByMBID = found %v, err %v; want the recording", found, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("facts = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMusicBrainzAdapter_RecordingByMBID_UnknownMBIDIsNotFound(t *testing.T) {
	a := recordingServer(t, nil)
	got, found, err := a.RecordingByMBID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil || found || got.MBID != "" {
		t.Errorf("unknown mbid = %+v, found %v, err %v; want not found without an error", got, found, err)
	}
}

func TestMusicBrainzAdapter_RecordingByMBID_BlankMBIDMakesNoRequest(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("blank mbid must not call MusicBrainz, got %s", r.URL)
	})
	if _, found, err := a.RecordingByMBID(context.Background(), "  "); found || err != nil {
		t.Errorf("blank mbid = found %v, err %v; want not found", found, err)
	}
}

func TestMusicBrainzAdapter_RecordingByMBID_ServerErrorIsError(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	if _, found, err := a.RecordingByMBID(context.Background(), bohemianMBID); found || err == nil {
		t.Errorf("a 503 = found %v, err %v; want an error", found, err)
	}
}

func TestMusicBrainzAdapter_RecordingByMBID_BodyWithoutIDIsNotFound(t *testing.T) {
	a := recordingServer(t, map[string]string{recordingLookup + bohemianMBID: `{"error":"Not Found"}`})
	if _, found, err := a.RecordingByMBID(context.Background(), bohemianMBID); found || err != nil {
		t.Errorf("body without id = found %v, err %v; want not found", found, err)
	}
}
