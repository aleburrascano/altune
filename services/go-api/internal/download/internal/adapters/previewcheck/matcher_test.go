package previewcheck_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func randomItems(seed uint64, count int) []uint32 {
	state := seed
	items := make([]uint32, count)
	for i := range items {
		state += 0x9e3779b97f4a7c15
		mixed := state
		mixed = (mixed ^ (mixed >> 30)) * 0xbf58476d1ce4e5b9
		mixed = (mixed ^ (mixed >> 27)) * 0x94d049bb133111eb
		items[i] = uint32((mixed ^ (mixed >> 31)) >> 32)
	}
	return items
}

type pathPrinter map[string][]uint32

func (p pathPrinter) Print(_ context.Context, audioPath string) ([]uint32, error) {
	if print, ok := p[audioPath]; ok {
		return print, nil
	}
	return p["preview"], nil
}

type printByCall struct{ prints [][]uint32 }

func (p *printByCall) Print(context.Context, string) ([]uint32, error) {
	next := p.prints[0]
	p.prints = p.prints[1:]
	return next, nil
}

func deezerServing(t *testing.T, trackBody func(server string) string) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/preview.mp3" {
			_, _ = w.Write([]byte("mp3 bytes"))
			return
		}
		_, _ = fmt.Fprint(w, trackBody(server.URL))
	}))
	t.Cleanup(server.Close)
	return server
}

func matcherFor(server *httptest.Server, prints download.RawFingerprint) download.PreviewMatcher {
	return download.NewPreviewMatcher(download.MustNewDeezerPreview(server.Client(), server.URL, prints, clock.Sleep, time.Now), prints, download.DefaultMatchMaxBitErrorRate)
}

func previewTrackBody(server string) string {
	return `{"id":132201194,"title":"outside","preview":"` + server + `/preview.mp3"}`
}

func TestMatcher_AFileContainingThePreviewMatches(t *testing.T) {
	preview := randomItems(1, 300)
	file := append(append(randomItems(2, 40), preview...), randomItems(3, 500)...)
	server := deezerServing(t, previewTrackBody)

	got, err := matcherFor(server, &printByCall{prints: [][]uint32{preview, file}}).
		MatchesPreview(context.Background(), "file.mp3", download.PreviewTarget{ISRC: "USSM11607554"})

	if err != nil || !got.Matched || got.BitErrorRate > 0.01 {
		t.Errorf("comparison = %+v, err = %v, want a match at about zero bit errors", got, err)
	}
}

func TestMatcher_AnotherSongDoesNotMatch(t *testing.T) {
	file := randomItems(9, 800)
	server := deezerServing(t, previewTrackBody)

	got, err := matcherFor(server, &printByCall{prints: [][]uint32{randomItems(1, 300), file}}).
		MatchesPreview(context.Background(), "file.mp3", download.PreviewTarget{DeezerTrackID: "132201194"})

	if err != nil || got.Matched || got.BitErrorRate < 0.35 {
		t.Errorf("comparison = %+v, err = %v, want no match and a rate near one half", got, err)
	}
}

func TestMatcher_SaysWhyItCouldNotCompare(t *testing.T) {
	cases := []struct {
		name   string
		body   func(string) string
		target download.PreviewTarget
		prints [][]uint32
		want   string
	}{
		{"no lookup key", previewTrackBody, download.PreviewTarget{}, nil, "no ISRC or Deezer track id to look a preview up by"},
		{"deezer has no preview", func(string) string { return `{"id":1,"title":"x","preview":""}` }, download.PreviewTarget{ISRC: "USSM11607554"}, nil, "deezer has no preview for this track"},
		{"the preview is too short", previewTrackBody, download.PreviewTarget{ISRC: "USSM11607554"}, [][]uint32{randomItems(1, 10), randomItems(2, 500)}, download.ErrPreviewTooShort.Error()},
		{"the file is longer than fpcalc reads", previewTrackBody, download.PreviewTarget{ISRC: "USSM11607554"}, [][]uint32{randomItems(1, 200), randomItems(2, download.FingerprintedItemCap)}, download.ErrFileReachesCap.Error()},
		{"the preview is longer than the file", previewTrackBody, download.PreviewTarget{ISRC: "USSM11607554"}, [][]uint32{randomItems(1, 300), randomItems(2, 200)}, download.ErrPrintLongerThanRef.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := deezerServing(t, tc.body)

			got, err := matcherFor(server, &printByCall{prints: tc.prints}).MatchesPreview(context.Background(), "file.mp3", tc.target)

			if err != nil || got.Matched || got.Unavailable != tc.want {
				t.Errorf("comparison = %+v, err = %v, want unavailable %q", got, err, tc.want)
			}
		})
	}
}

func TestMatcher_AThrottledDeezerIsAnError(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := matcherFor(server, pathPrinter{}).MatchesPreview(ctx, "file.mp3", download.PreviewTarget{ISRC: "USSM11607554"})

	if !errors.Is(err, context.DeadlineExceeded) || got.Matched {
		t.Errorf("MatchesPreview = %+v, %v; want the call to wait out the throttling until the caller's deadline ends it", got, err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("deezer asked %d times, want once: a throttled deezer is backed off from, not hammered", n)
	}
}
