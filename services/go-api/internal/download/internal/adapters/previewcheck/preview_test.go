package previewcheck_test

import (
	"altune/go-api/internal/download/internal/adapters/previewcheck"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const fpcalcFails = "#!/bin/sh\necho cannot decode >&2\nexit 1\n"

const deezer = "https://deezer.test"

const quota = `{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`

const noPreview = `{"id":1,"title":"t","preview":""}`

func writeFakeFpcalc(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fpcalc"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake fpcalc: %v", err)
	}
	return filepath.Join(dir, "fpcalc")
}

type fixedPrinter struct {
	print []uint32
	err   error
}

func (f fixedPrinter) Print(context.Context, string) ([]uint32, error) { return f.print, f.err }

type trackRequest struct {
	path string
	at   time.Time
}

type deezerStub struct {
	mu        sync.Mutex
	tracks    []trackRequest
	trackBody func(request int) string
	previews  int
}

func (s *deezerStub) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body := "mp3 bytes"
	if r.URL.Path == "/preview.mp3" {
		s.previews++
	} else {
		s.tracks = append(s.tracks, trackRequest{path: r.URL.Path, at: time.Now()})
		body = s.trackBody(len(s.tracks))
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func (s *deezerStub) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := make([]string, 0, len(s.tracks))
	for _, request := range s.tracks {
		paths = append(paths, request.path)
	}
	return paths
}

func (s *deezerStub) times() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	times := make([]time.Time, 0, len(s.tracks))
	for _, request := range s.tracks {
		times = append(times, request.at)
	}
	return times
}

type previewSource interface {
	Clip(ctx context.Context, identity previewcheck.Target) (previewcheck.Clip, error)
}

func (s *deezerStub) serve() previewSource {
	return previewcheck.MustNew(&http.Client{Transport: s}, deezer, fixedPrinter{print: []uint32{7, 8, 9}}, clock.Sleep, time.Now)
}

func TestNewDeezerPreview_RefusesAMissingClock(t *testing.T) {
	previews, err := previewcheck.New(&http.Client{}, deezer, fixedPrinter{}, clock.Sleep, nil)

	if previews != nil || !errors.Is(err, previewcheck.ErrClockRequired) || !strings.Contains(err.Error(), "NewDeezerPreview") {
		t.Fatalf("got (%v, %v), want a nil client and an error naming NewDeezerPreview wrapping ErrDeezerClockRequired", previews, err)
	}
}

const trackJSON = `{"id":132201194,"title":"outside","isrc":"USSM11607554","preview":"` + deezer + `/preview.mp3"}`

func TestNewDeezerPreview_RefusesAMissingSleeper(t *testing.T) {
	previews, err := previewcheck.New(&http.Client{}, deezer, fixedPrinter{}, nil, time.Now)

	if previews != nil || !errors.Is(err, previewcheck.ErrSleeperRequired) || !strings.Contains(err.Error(), "NewDeezerPreview") {
		t.Fatalf("got (%v, %v), want a nil client and an error naming NewDeezerPreview wrapping ErrDeezerSleeperRequired", previews, err)
	}
}

func TestDeezerPreviews_FetchesAndFingerprintsThePreviewOfTheBoundISRC(t *testing.T) {
	stub := &deezerStub{trackBody: func(int) string { return trackJSON }}

	clip, err := stub.serve().Clip(context.Background(), previewcheck.Target{ISRC: "ussm1-16-07554", DeezerTrackID: "999"})
	if err != nil {
		t.Fatalf("Clip: %v", err)
	}
	if clip.DeezerTrackID != "132201194" || clip.DeezerTitle != "outside" || len(clip.Print) != 3 {
		t.Errorf("clip = %+v, want deezer track 132201194 titled outside with the printed fingerprint", clip)
	}
	if paths := stub.paths(); len(paths) != 1 || paths[0] != "/track/isrc:USSM11607554" || stub.previews != 1 {
		t.Errorf("requests = %v with %d preview downloads, want the canonical ISRC lookup then one download", paths, stub.previews)
	}
}

func TestDeezerPreviews_FallsBackToTheDeezerTrackIDWithoutAnISRC(t *testing.T) {
	stub := &deezerStub{trackBody: func(int) string { return trackJSON }}

	if _, err := stub.serve().Clip(context.Background(), previewcheck.Target{DeezerTrackID: "132201194"}); err != nil {
		t.Fatalf("Clip: %v", err)
	}

	if paths := stub.paths(); len(paths) != 1 || paths[0] != "/track/132201194" {
		t.Errorf("requests = %v, want a lookup by the deezer track id", paths)
	}
}

func TestDeezerPreviews_SaysWhyThereIsNoPreview(t *testing.T) {
	tests := []struct {
		name     string
		identity previewcheck.Target
		body     string
		want     string
	}{
		{"no ISRC and no deezer id", previewcheck.Target{}, "", "no ISRC or Deezer track id to look a preview up by"},
		{"deezer does not know the ISRC", previewcheck.Target{ISRC: "USSM11607554"}, `{"error":{"type":"DataException","message":"no data","code":800}}`, "deezer does not know this track"},
		{"deezer knows the track but has no preview", previewcheck.Target{ISRC: "USSM11607554"}, `{"id":1,"title":"outside","preview":""}`, "deezer has no preview for this track"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &deezerStub{trackBody: func(int) string { return tt.body }}

			clip, err := stub.serve().Clip(context.Background(), tt.identity)

			if err != nil || clip.Unavailable != tt.want || len(clip.Print) != 0 {
				t.Errorf("clip = %+v, err = %v, want unavailable %q", clip, err, tt.want)
			}
		})
	}
}

func TestDeezerPreviews_BacksOffTenSecondsThenTwentyOnQuotaErrorsAndThenSucceeds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := &deezerStub{trackBody: func(request int) string {
			if request < 3 {
				return quota
			}
			return trackJSON
		}}
		started := time.Now()

		clip, err := stub.serve().Clip(context.Background(), previewcheck.Target{ISRC: "USSM11607554"})

		if err != nil || clip.DeezerTrackID != "132201194" {
			t.Fatalf("clip = %+v, err = %v, want the track after two quota errors", clip, err)
		}
		want := []time.Duration{0, 10 * time.Second, 30 * time.Second}
		times := stub.times()
		if len(times) != len(want) {
			t.Fatalf("track requests = %d, want %d", len(times), len(want))
		}
		for i, at := range times {
			if got := at.Sub(started); got != want[i] {
				t.Errorf("track request %d went out at +%v, want +%v", i+1, got, want[i])
			}
		}
	})
}

func TestDeezerPreviews_GivesUpAfterFiveAttemptsWhenQuotaNeverLifts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := &deezerStub{trackBody: func(int) string { return `{"error":{"code":4}}` }}

		_, err := stub.serve().Clip(context.Background(), previewcheck.Target{ISRC: "USSM11607554"})

		if !errors.Is(err, previewcheck.ErrDeezerThrottled) {
			t.Errorf("err = %v, want ErrDeezerThrottled", err)
		}
		if paths := stub.paths(); len(paths) != 5 {
			t.Errorf("track requests = %d, want 5", len(paths))
		}
	})
}

func TestDeezerPreviews_AnHTTP429EndsTheCallAtOnceAndHoldsTheNextCallerToTheCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := &throttlingOnce{deezerStub: deezerStub{trackBody: func(int) string { return noPreview }}}
		previews := previewcheck.MustNew(&http.Client{Transport: stub}, deezer, fixedPrinter{}, clock.Sleep, time.Now)
		started := time.Now()

		_, err := previews.Clip(context.Background(), previewcheck.Target{ISRC: "USSM11607554"})

		if !errors.Is(err, previewcheck.ErrDeezerThrottled) {
			t.Fatalf("err = %v, want ErrDeezerThrottled: the egress transport has already retried the 429, so this layer must not loop on it again", err)
		}
		if stub.throttledCalls != 1 {
			t.Errorf("upstream saw %d throttled requests from this layer, want 1", stub.throttledCalls)
		}
		clip, err := previews.Clip(context.Background(), previewcheck.Target{ISRC: "USSM11607554"})
		if err != nil || clip.Unavailable == "" {
			t.Fatalf("next caller: clip = %+v, err = %v, want the answer once the cooldown is over", clip, err)
		}
		if times := stub.times(); len(times) != 1 || times[0].Sub(started) != 10*time.Second {
			t.Errorf("track requests after the 429 = %v, want one at +10s: the 429 must set the shared cooldown", times)
		}
	})
}

type throttlingOnce struct {
	deezerStub
	throttled      bool
	throttledCalls int
}

func (s *throttlingOnce) RoundTrip(r *http.Request) (*http.Response, error) {
	if !s.throttled {
		s.throttled = true
		s.throttledCalls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: http.NoBody, Request: r}, nil
	}
	return s.deezerStub.RoundTrip(r)
}

func TestDeezerPreviews_HoldsEveryRequestWhileAnotherCallerIsBackingOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := &deezerStub{trackBody: func(request int) string {
			if request == 1 {
				return quota
			}
			return noPreview
		}}
		previews := stub.serve()
		started := time.Now()
		backingOff := make(chan struct{})
		go func() {
			defer close(backingOff)
			_, _ = previews.Clip(context.Background(), previewcheck.Target{ISRC: "USSM11607554"})
		}()
		time.Sleep(time.Second)

		_, _ = previews.Clip(context.Background(), previewcheck.Target{DeezerTrackID: "7"})

		<-backingOff
		if paths := stub.paths(); len(paths) != 3 {
			t.Fatalf("track requests = %v, want the quota error, its retry and the held caller's", paths)
		}
		for _, at := range stub.times()[1:] {
			if got := at.Sub(started); got < 10*time.Second {
				t.Errorf("a track request went out at +%v, want every one held to the +10s cooldown", got)
			}
		}
	})
}

func TestFpcalcPrinter_ReadsTheRawFingerprint(t *testing.T) {
	fpcalc := writeFakeFpcalc(t, "#!/bin/sh\nprintf '{\"duration\": 30, \"fingerprint\": [1936120799,4058316077,3]}'\n")

	got, err := previewcheck.NewFpcalcPrinter(fpcalc).Print(context.Background(), os.DevNull)

	if err != nil || len(got) != 3 || got[0] != 1936120799 || got[1] != 4058316077 {
		t.Errorf("print = %v, err = %v, want the three raw items", got, err)
	}
}

func TestFpcalcPrinter_RefusesAnEmptyOrFailedFingerprint(t *testing.T) {
	empty := writeFakeFpcalc(t, "#!/bin/sh\nprintf '{\"duration\": 30, \"fingerprint\": []}'\n")
	failing := writeFakeFpcalc(t, fpcalcFails)

	if _, err := previewcheck.NewFpcalcPrinter(empty).Print(context.Background(), os.DevNull); !errors.Is(err, previewcheck.ErrEmptyPrint) {
		t.Errorf("empty err = %v, want ErrEmptyPrint", err)
	}
	if _, err := previewcheck.NewFpcalcPrinter(failing).Print(context.Background(), os.DevNull); err == nil {
		t.Error("failing fpcalc returned no error")
	}
}
