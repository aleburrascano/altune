package musicbrainz

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"net/http"
	"reflect"
	"testing"
)

const (
	itsOnlyLoveRecording = "7933a65c-2c66-4ec1-9880-e46b5d2f05de"
	itsOnlyLoveWork      = "102878cf-2ac2-4088-b228-c57193b5cdba"
	classicVersionMBID   = "25d68187-ee71-4f06-9e58-0b49d66bf8a8"
	classicDupMBID       = "e5cdd7d1-727a-4a5d-9318-11a1cb8e19dc"
	hammersmithLiveMBID  = "8d393956-4a15-4bf8-982b-d4e0aea20550"
	bryanAdamsID         = "4dbf5678-7a31-406a-abbe-232f8ac2cd63"
	tinaTurnerID         = "9072df14-b61e-42e2-b4f4-6bbb7fdb5586"

	itsOnlyLoveRecordingRels = `{"id":"7933a65c-2c66-4ec1-9880-e46b5d2f05de","title":"It’s Only Love","length":195000,"relations":[{"type":"performance","target-type":"work","work":{"id":"102878cf-2ac2-4088-b228-c57193b5cdba","title":"It’s Only Love"}}]}`
	itsOnlyLoveWorkRels      = `{"id":"102878cf-2ac2-4088-b228-c57193b5cdba","relations":[
{"target-type":"recording","attributes":[],"recording":{"id":"7933a65c-2c66-4ec1-9880-e46b5d2f05de","title":"It’s Only Love","length":195000,"disambiguation":"","video":false,"artist-credit":[{"name":"Bryan Adams","joinphrase":" & ","artist":{"id":"4dbf5678-7a31-406a-abbe-232f8ac2cd63","name":"Bryan Adams"}},{"name":"Tina Turner","joinphrase":"","artist":{"id":"9072df14-b61e-42e2-b4f4-6bbb7fdb5586","name":"Tina Turner"}}]}},
{"target-type":"recording","attributes":[],"recording":{"id":"25d68187-ee71-4f06-9e58-0b49d66bf8a8","title":"It’s Only Love (classic version)","length":194000,"disambiguation":"","video":false,"artist-credit":[{"name":"Bryan Adams","joinphrase":" & ","artist":{"id":"4dbf5678-7a31-406a-abbe-232f8ac2cd63","name":"Bryan Adams"}},{"name":"Tina Turner","joinphrase":"","artist":{"id":"9072df14-b61e-42e2-b4f4-6bbb7fdb5586","name":"Tina Turner"}}]}},
{"target-type":"recording","attributes":["live"],"recording":{"id":"8d393956-4a15-4bf8-982b-d4e0aea20550","title":"It’s Only Love","length":null,"disambiguation":"live, 1985-04: Hammersmith Odeon, London, England, UK","video":false,"artist-credit":[{"name":"Bryan Adams","joinphrase":"","artist":{"id":"4dbf5678-7a31-406a-abbe-232f8ac2cd63","name":"Bryan Adams"}}]}},
{"target-type":"recording","attributes":[],"recording":{"id":"25d68187-ee71-4f06-9e58-0b49d66bf8a8","title":"It’s Only Love (classic version)","length":194000,"disambiguation":"","video":false,"artist-credit":[]}}
]}`
	classicDetail = `{"id":"25d68187-ee71-4f06-9e58-0b49d66bf8a8","title":"It’s Only Love (classic version)","length":194000,"first-release-date":"2025-04-25","isrcs":["IETLH2500023"],"artist-credit":[{"name":"Bryan Adams","joinphrase":" & ","artist":{"id":"4dbf5678-7a31-406a-abbe-232f8ac2cd63","name":"Bryan Adams"}},{"name":"Tina Turner","joinphrase":"","artist":{"id":"9072df14-b61e-42e2-b4f4-6bbb7fdb5586","name":"Tina Turner"}}],"releases":[{"title":"It’s Only Love","date":"2025-04-25","status":"Official"}]}`
)

func workServer(t *testing.T, status int) *MusicBrainzAdapter {
	t.Helper()
	return mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		inc := r.URL.Query().Get("inc")
		switch {
		case r.URL.Path == recordingLookup+itsOnlyLoveRecording && inc == "work-rels artist-credits":
			_, _ = w.Write([]byte(itsOnlyLoveRecordingRels))
		case r.URL.Path == "/ws/2/work/"+itsOnlyLoveWork && inc == "recording-rels artist-credits":
			_, _ = w.Write([]byte(itsOnlyLoveWorkRels))
		case r.URL.Path == recordingLookup+classicVersionMBID && inc == "isrcs artist-credits releases":
			_, _ = w.Write([]byte(classicDetail))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestMusicBrainzAdapter_WorkRecordings(t *testing.T) {
	a := workServer(t, http.StatusOK)

	got, err := a.WorkRecordings(context.Background(), itsOnlyLoveRecording)
	if err != nil {
		t.Fatal(err)
	}
	want := []ports.MBWorkRecording{
		{MBID: classicVersionMBID, Title: "It’s Only Love (classic version)", ArtistIDs: []string{bryanAdamsID, tinaTurnerID}, Duration: 194},
		{MBID: hammersmithLiveMBID, Title: "It’s Only Love", Disambiguation: "live, 1985-04: Hammersmith Odeon, London, England, UK", ArtistIDs: []string{bryanAdamsID}, Live: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMusicBrainzAdapter_WorkRecordings_unknownRecording(t *testing.T) {
	a := workServer(t, http.StatusOK)
	got, err := a.WorkRecordings(context.Background(), classicDupMBID)
	if got != nil || err != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestMusicBrainzAdapter_WorkRecordings_serverError(t *testing.T) {
	a := workServer(t, http.StatusInternalServerError)
	if _, err := a.WorkRecordings(context.Background(), itsOnlyLoveRecording); err == nil {
		t.Fatal("want an error for a 5xx")
	}
}

func TestMusicBrainzAdapter_RecordingDetail(t *testing.T) {
	a := workServer(t, http.StatusOK)

	got, ok, err := a.RecordingDetail(context.Background(), classicVersionMBID)
	if err != nil || !ok {
		t.Fatalf("got ok=%v err=%v", ok, err)
	}
	want := ports.MBRecordingDetail{
		MBID: classicVersionMBID, Title: "It’s Only Love (classic version)",
		ArtistIDs: []string{bryanAdamsID, tinaTurnerID}, ISRCs: []string{"IETLH2500023"},
		Duration: 194, FirstReleaseDate: "2025-04-25",
		Releases: []ports.MBRelease{{Title: "It’s Only Love", Date: "2025-04-25", Status: "Official"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if _, ok, err := a.RecordingDetail(context.Background(), classicDupMBID); ok || err != nil {
		t.Fatalf("unknown: ok=%v err=%v", ok, err)
	}
}

func probeServer(t *testing.T, bodies map[string]string) *MusicBrainzAdapter {
	t.Helper()
	return mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, known := bodies[r.URL.Path+"?"+r.URL.Query().Get("inc")]
		switch {
		case !known:
			w.WriteHeader(http.StatusNotFound)
		case body == "500":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			_, _ = w.Write([]byte(body))
		}
	})
}

const (
	probeRec   = "aaaaaaaa-0000-0000-0000-000000000001"
	probeWorkA = "bbbbbbbb-0000-0000-0000-00000000000a"
	probeWorkB = "bbbbbbbb-0000-0000-0000-00000000000b"
	probeWorkC = "bbbbbbbb-0000-0000-0000-00000000000c"
	probeX     = "cccccccc-0000-0000-0000-000000000001"
	probeY     = "cccccccc-0000-0000-0000-000000000002"
	probeZ     = "cccccccc-0000-0000-0000-000000000003"

	probeRecOneWork = `{"id":"` + probeRec + `","relations":[{"type":"performance","target-type":"work","work":{"id":"` + probeWorkA + `"}}]}`
	probeRecPath    = recordingLookup + probeRec + "?work-rels artist-credits"
	probeWorkAPath  = "/ws/2/work/" + probeWorkA + "?recording-rels artist-credits"
)

func probeWorkBody(id string, recs ...string) string {
	body := `{"id":"` + id + `","relations":[`
	for i, r := range recs {
		if i > 0 {
			body += ","
		}
		body += `{"target-type":"recording","attributes":[],"recording":{"id":"` + r + `","title":"T","length":100000,"disambiguation":"","artist-credit":[{"name":"A","artist":{"id":"` + bryanAdamsID + `"}}]}}`
	}
	return body + "]}"
}

func TestMusicBrainzAdapter_WorkRecordings_listsEveryPerformanceWorkOnceEach(t *testing.T) {
	a := probeServer(t, map[string]string{
		recordingLookup + probeRec + "?work-rels artist-credits": `{"id":"` + probeRec + `","title":"T","relations":[
{"type":"performance","target-type":"work","work":{"id":"` + probeWorkA + `"}},
{"type":"other","target-type":"work","work":{"id":"` + probeWorkC + `"}},
{"type":"performance","target-type":"work","work":{"id":"` + probeWorkB + `"}}]}`,
		"/ws/2/work/" + probeWorkA + "?recording-rels artist-credits": probeWorkBody(probeWorkA, probeX, probeRec, probeY),
		"/ws/2/work/" + probeWorkB + "?recording-rels artist-credits": probeWorkBody(probeWorkB, probeY, probeZ),
		"/ws/2/work/" + probeWorkC + "?recording-rels artist-credits": "500",
	})

	got, err := a.WorkRecordings(context.Background(), probeRec)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range got {
		ids = append(ids, r.MBID)
	}
	if want := []string{probeX, probeY, probeZ}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
}

func TestMusicBrainzAdapter_WorkRecordings_recordingWithNoWorkHasNoOtherRecordings(t *testing.T) {
	a := probeServer(t, map[string]string{
		recordingLookup + probeRec + "?work-rels artist-credits": `{"id":"` + probeRec + `","title":"T","relations":[]}`,
	})
	got, err := a.WorkRecordings(context.Background(), probeRec)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none, nil", got, err)
	}
}

func TestMusicBrainzAdapter_WorkRecordings_malformedResponseIsAnError(t *testing.T) {
	cases := map[string]map[string]string{
		"recording": {recordingLookup + probeRec + "?work-rels artist-credits": `{"id":`},
		"work": {
			probeRecPath:   probeRecOneWork,
			probeWorkAPath: `<html>oops</html>`,
		},
	}
	for name, bodies := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := probeServer(t, bodies).WorkRecordings(context.Background(), probeRec); err == nil {
				t.Fatal("want an error for a malformed body")
			}
		})
	}
}

func TestMusicBrainzAdapter_WorkRecordings_workServerErrorIsAnError(t *testing.T) {
	a := probeServer(t, map[string]string{
		probeRecPath:   probeRecOneWork,
		probeWorkAPath: "500",
	})
	if _, err := a.WorkRecordings(context.Background(), probeRec); err == nil {
		t.Fatal("want an error when the work lookup 5xxs")
	}
}

func TestMusicBrainzAdapter_cancelledContextIsAnError(t *testing.T) {
	a := workServer(t, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := a.WorkRecordings(ctx, itsOnlyLoveRecording); err == nil {
		t.Fatalf("WorkRecordings: got %v, nil; want an error", got)
	}
	if _, ok, err := a.RecordingDetail(ctx, classicVersionMBID); err == nil || ok {
		t.Fatalf("RecordingDetail: ok=%v err=%v; want an error", ok, err)
	}
}

func TestMusicBrainzAdapter_RecordingDetail_recordingWithoutISRCOrLength(t *testing.T) {
	a := probeServer(t, map[string]string{
		recordingLookup + classicDupMBID + "?isrcs artist-credits releases": `{"id":"e5cdd7d1-727a-4a5d-9318-11a1cb8e19dc","title":"It’s Only Love (classic version)","length":null,"isrcs":[],"artist-credit":[{"name":"Bryan Adams","artist":{"id":"4dbf5678-7a31-406a-abbe-232f8ac2cd63"}}],"releases":[{"title":"Bryan Adams: Classic","date":"2025-05-09","status":"Official"}]}`,
	})
	got, ok, err := a.RecordingDetail(context.Background(), classicDupMBID)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(got.ISRCs) != 0 || got.Duration != 0 || got.FirstReleaseDate != "" {
		t.Fatalf("got ISRCs=%v Duration=%v FirstReleaseDate=%q; want none, 0, \"\"", got.ISRCs, got.Duration, got.FirstReleaseDate)
	}
	if want := []ports.MBRelease{{Title: "Bryan Adams: Classic", Date: "2025-05-09", Status: "Official"}}; !reflect.DeepEqual(got.Releases, want) {
		t.Fatalf("releases %+v, want %+v", got.Releases, want)
	}
	if got.MBID != classicDupMBID || !reflect.DeepEqual(got.ArtistIDs, []string{bryanAdamsID}) {
		t.Fatalf("got MBID=%q ArtistIDs=%v", got.MBID, got.ArtistIDs)
	}
}

func TestMusicBrainzAdapter_RecordingDetail_serverErrorOrMalformedIsAnError(t *testing.T) {
	for name, body := range map[string]string{"5xx": "500", "malformed": `{"isrcs":`} {
		t.Run(name, func(t *testing.T) {
			a := probeServer(t, map[string]string{recordingLookup + classicVersionMBID + "?isrcs artist-credits releases": body})
			if _, ok, err := a.RecordingDetail(context.Background(), classicVersionMBID); err == nil || ok {
				t.Fatalf("ok=%v err=%v; want an error", ok, err)
			}
		})
	}
}
