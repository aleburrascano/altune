package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

type fixedPrinter struct {
	print []uint32
	err   error
}

func (f fixedPrinter) Print(context.Context, string) ([]uint32, error) { return f.print, f.err }

type deezerStub struct {
	mu        sync.Mutex
	paths     []string
	trackBody func(request int) string
	previews  int
}

func (s *deezerStub) serve(t *testing.T) *deezerPreviews {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path == "/preview.mp3" {
			s.previews++
			_, _ = w.Write([]byte("mp3 bytes"))
			return
		}
		s.paths = append(s.paths, r.URL.Path)
		_, _ = fmt.Fprint(w, s.trackBody(len(s.paths)))
	}))
	t.Cleanup(server.Close)
	previews := newDeezerPreviews(server.Client(), server.URL, 1000, fixedPrinter{print: []uint32{7, 8, 9}})
	previews.backoff = time.Millisecond
	return previews
}

func trackJSON(server string) string {
	return `{"id":132201194,"title":"outside","isrc":"USSM11607554","preview":"` + server + `/preview.mp3"}`
}

func TestDeezerPreviews_FetchesAndFingerprintsThePreviewOfTheBoundISRC(t *testing.T) {
	stub := &deezerStub{}
	var base string
	stub.trackBody = func(int) string { return trackJSON(base) }
	previews := stub.serve(t)
	base = previews.baseURL

	clip, err := previews.Clip(context.Background(), boundIdentity{ISRC: "ussm1-16-07554", DeezerTrackID: "999"})
	if err != nil {
		t.Fatalf("Clip: %v", err)
	}
	if clip.DeezerTrackID != "132201194" || clip.DeezerTitle != "outside" || len(clip.Print) != 3 {
		t.Errorf("clip = %+v, want deezer track 132201194 titled outside with the printed fingerprint", clip)
	}
	if len(stub.paths) != 1 || stub.paths[0] != "/track/isrc:USSM11607554" || stub.previews != 1 {
		t.Errorf("requests = %v with %d preview downloads, want the canonical ISRC lookup then one download", stub.paths, stub.previews)
	}
}

func TestDeezerPreviews_FallsBackToTheDeezerTrackIDWithoutAnISRC(t *testing.T) {
	stub := &deezerStub{}
	var base string
	stub.trackBody = func(int) string { return trackJSON(base) }
	previews := stub.serve(t)
	base = previews.baseURL

	if _, err := previews.Clip(context.Background(), boundIdentity{DeezerTrackID: "132201194"}); err != nil {
		t.Fatalf("Clip: %v", err)
	}

	if len(stub.paths) != 1 || stub.paths[0] != "/track/132201194" {
		t.Errorf("requests = %v, want a lookup by the deezer track id", stub.paths)
	}
}

func TestDeezerPreviews_SaysWhyThereIsNoPreview(t *testing.T) {
	tests := []struct {
		name     string
		identity boundIdentity
		body     string
		want     string
	}{
		{"no ISRC and no deezer id", boundIdentity{}, "", noLookupKey},
		{"deezer does not know the ISRC", boundIdentity{ISRC: "USSM11607554"}, `{"error":{"type":"DataException","message":"no data","code":800}}`, unknownToDeezer},
		{"deezer knows the track but has no preview", boundIdentity{ISRC: "USSM11607554"}, `{"id":1,"title":"outside","preview":""}`, noPreviewAtDeezer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &deezerStub{trackBody: func(int) string { return tt.body }}
			previews := stub.serve(t)

			clip, err := previews.Clip(context.Background(), tt.identity)

			if err != nil || clip.Unavailable != tt.want || len(clip.Print) != 0 {
				t.Errorf("clip = %+v, err = %v, want unavailable %q", clip, err, tt.want)
			}
		})
	}
}

func TestDeezerPreviews_BacksOffOnAQuotaErrorAndThenSucceeds(t *testing.T) {
	stub := &deezerStub{}
	var base string
	quota := `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`
	stub.trackBody = func(request int) string {
		if request < 3 {
			return quota
		}
		return trackJSON(base)
	}
	previews := stub.serve(t)
	base = previews.baseURL

	clip, err := previews.Clip(context.Background(), boundIdentity{ISRC: "USSM11607554"})

	if err != nil || clip.DeezerTrackID != "132201194" {
		t.Fatalf("clip = %+v, err = %v, want the track after two quota errors", clip, err)
	}
	if len(stub.paths) != 3 {
		t.Errorf("track requests = %d, want 3", len(stub.paths))
	}
}

func TestDeezerPreviews_GivesUpWhenQuotaNeverLifts(t *testing.T) {
	stub := &deezerStub{trackBody: func(int) string { return `{"error":{"code":4}}` }}
	previews := stub.serve(t)

	_, err := previews.Clip(context.Background(), boundIdentity{ISRC: "USSM11607554"})

	if !errors.Is(err, errDeezerThrottled) {
		t.Errorf("err = %v, want errDeezerThrottled", err)
	}
	if len(stub.paths) != deezerAttempts {
		t.Errorf("track requests = %d, want %d", len(stub.paths), deezerAttempts)
	}
}

func TestDeezerPreviews_HoldsEveryRequestWhileAnotherCallerIsBackingOff(t *testing.T) {
	stub := &deezerStub{trackBody: func(int) string { return `{"id":1,"title":"t","preview":""}` }}
	previews := stub.serve(t)
	const pause = 120 * time.Millisecond
	previews.pause.extendTo(time.Now().Add(pause))
	started := time.Now()

	_, _ = previews.Clip(context.Background(), boundIdentity{ISRC: "USSM11607554"})

	if elapsed := time.Since(started); elapsed < pause-10*time.Millisecond {
		t.Errorf("request went out after %v, want it held for the %v cooldown", elapsed, pause)
	}
}

func TestFpcalcPrinter_ReadsTheRawFingerprint(t *testing.T) {
	dir := writeFakeFpcalc(t, "#!/bin/sh\nprintf '{\"duration\": 30, \"fingerprint\": [1936120799,4058316077,3]}'\n")

	got, err := fpcalcPrinter{binary: dir + "/fpcalc"}.Print(context.Background(), os.DevNull)

	if err != nil || len(got) != 3 || got[0] != 1936120799 || got[1] != 4058316077 {
		t.Errorf("print = %v, err = %v, want the three raw items", got, err)
	}
}

func TestFpcalcPrinter_RefusesAnEmptyOrFailedFingerprint(t *testing.T) {
	empty := writeFakeFpcalc(t, "#!/bin/sh\nprintf '{\"duration\": 30, \"fingerprint\": []}'\n")
	failing := writeFakeFpcalc(t, fpcalcFails)

	if _, err := (fpcalcPrinter{binary: empty + "/fpcalc"}).Print(context.Background(), os.DevNull); !errors.Is(err, errEmptyPrint) {
		t.Errorf("empty err = %v, want errEmptyPrint", err)
	}
	if _, err := (fpcalcPrinter{binary: failing + "/fpcalc"}).Print(context.Background(), os.DevNull); err == nil {
		t.Error("failing fpcalc returned no error")
	}
}
