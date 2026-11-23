package pacing_test

import (
	"altune/go-api/internal/shared/egress/internal/pacing"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const shortDeadline = 50 * time.Millisecond

func waitsWithin(t *testing.T, p *pacing.Pacer, host string, requests int) []error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), shortDeadline)
	defer cancel()
	errs := make([]error, requests)
	for i := range errs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		errs[i] = p.Wait(req)
	}
	return errs
}

func assertBurstThenRefusal(t *testing.T, p *pacing.Pacer, host string, burst int) {
	t.Helper()
	errs := waitsWithin(t, p, host, burst+1)
	for i, err := range errs[:burst] {
		if err != nil {
			t.Errorf("%s: request %d of a burst of %d waited: %v", host, i+1, burst, err)
		}
	}
	if errs[burst] == nil {
		t.Errorf("%s: request %d passed at once, want it paced past a %v deadline", host, burst+1, shortDeadline)
	}
}

func TestPacer_MusicBrainzAllowsNoBurst(t *testing.T) {
	assertBurstThenRefusal(t, pacing.New(), "musicbrainz.org", 1)
}

func TestPacer_OtherProvidersAllowABurstOfFour(t *testing.T) {
	assertBurstThenRefusal(t, pacing.New(), "api.deezer.com", 4)
}

func TestPacer_LeavesUnlistedHostsUnpaced(t *testing.T) {
	p := pacing.New()
	for i, err := range waitsWithin(t, p, "unlisted.example.com", 100) {
		if err != nil {
			t.Fatalf("request %d to an unlisted host waited: %v", i+1, err)
		}
	}
}

func TestPacer_KeepsEachHostsBudgetApart(t *testing.T) {
	p := pacing.New()
	waitsWithin(t, p, "musicbrainz.org", 2)

	assertBurstThenRefusal(t, p, "api.deezer.com", 4)
}

func TestPacer_StopsWaitingWhenTheRequestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://musicbrainz.org/x", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := pacing.New().Wait(req); err == nil {
		t.Fatal("a cancelled request to a paced host was let through")
	}
}

var notFetchedHosts = map[string]bool{"e-cdns-images.dzcdn.net": true, "www.youtube.com": true}

var adapterURLHost = regexp.MustCompile(`"https?://([a-zA-Z0-9.-]+)`)

var inMemoryFakeAdapterDirsThatNeverFetch = map[string]bool{"memory": true}

func adapterHosts(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "discovery", "internal", "adapters")
	seen := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && inMemoryFakeAdapterDirsThatNeverFetch[d.Name()] {
			return fs.SkipDir
		}
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, m := range adapterURLHost.FindAllSubmatch(src, -1) {
			if !notFetchedHosts[string(m[1])] {
				seen[string(m[1])] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hosts := make([]string, 0, len(seen))
	for h := range seen {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

func TestPacer_PacesEveryWiredProviderHost(t *testing.T) {
	hosts := adapterHosts(t)
	if len(hosts) == 0 {
		t.Fatal("found no adapter URL hosts to check")
	}
	p := pacing.New()
	for _, h := range hosts {
		burst := 4
		if h == "musicbrainz.org" {
			burst = 1
		}
		assertBurstThenRefusal(t, p, h, burst)
	}
}
