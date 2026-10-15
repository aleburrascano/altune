package deezerpreview

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func randomItems(seed uint64, count int) []uint32 {
	source := rand.New(rand.NewPCG(seed, seed))
	items := make([]uint32, count)
	for i := range items {
		items[i] = source.Uint32()
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

func matcherFor(server *httptest.Server, prints RawPrinter) *Matcher {
	previews := New(server.Client(), server.URL, 1000, prints)
	previews.backoff = time.Millisecond
	return NewMatcher(previews, prints, DefaultMatchMaxBitErrorRate)
}

func previewTrackBody(server string) string {
	return `{"id":132201194,"title":"outside","preview":"` + server + `/preview.mp3"}`
}

func TestMatcher_AFileContainingThePreviewMatches(t *testing.T) {
	preview := randomItems(1, 300)
	file := append(append(randomItems(2, 40), preview...), randomItems(3, 500)...)
	server := deezerServing(t, previewTrackBody)

	got, err := matcherFor(server, &printByCall{prints: [][]uint32{preview, file}}).
		MatchesPreview(context.Background(), "file.mp3", ports.PreviewTarget{ISRC: "USSM11607554"})

	if err != nil || !got.Matched || got.BitErrorRate > 0.01 {
		t.Errorf("comparison = %+v, err = %v, want a match at about zero bit errors", got, err)
	}
}

func TestMatcher_AnotherSongDoesNotMatch(t *testing.T) {
	file := randomItems(9, 800)
	server := deezerServing(t, previewTrackBody)

	got, err := matcherFor(server, &printByCall{prints: [][]uint32{randomItems(1, 300), file}}).
		MatchesPreview(context.Background(), "file.mp3", ports.PreviewTarget{DeezerTrackID: "132201194"})

	if err != nil || got.Matched || got.BitErrorRate < 0.35 {
		t.Errorf("comparison = %+v, err = %v, want no match and a rate near one half", got, err)
	}
}

func TestMatcher_SaysWhyItCouldNotCompare(t *testing.T) {
	cases := []struct {
		name   string
		body   func(string) string
		target ports.PreviewTarget
		prints [][]uint32
		want   string
	}{
		{"no lookup key", previewTrackBody, ports.PreviewTarget{}, nil, noLookupKey},
		{"deezer has no preview", func(string) string { return `{"id":1,"title":"x","preview":""}` }, ports.PreviewTarget{ISRC: "USSM11607554"}, nil, noPreviewAtDeezer},
		{"the preview is too short", previewTrackBody, ports.PreviewTarget{ISRC: "USSM11607554"}, [][]uint32{randomItems(1, 10), randomItems(2, 500)}, ErrPreviewTooShort.Error()},
		{"the file is longer than fpcalc reads", previewTrackBody, ports.PreviewTarget{ISRC: "USSM11607554"}, [][]uint32{randomItems(1, 200), randomItems(2, FingerprintedItemCap)}, ErrFileReachesCap.Error()},
		{"the preview is longer than the file", previewTrackBody, ports.PreviewTarget{ISRC: "USSM11607554"}, [][]uint32{randomItems(1, 300), randomItems(2, 200)}, ErrPrintLongerThanRef.Error()},
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	_, err := matcherFor(server, pathPrinter{}).MatchesPreview(context.Background(), "file.mp3", ports.PreviewTarget{ISRC: "USSM11607554"})

	if err == nil {
		t.Error("MatchesPreview = nil, want the throttling reported so the caller can retry later")
	}
}
