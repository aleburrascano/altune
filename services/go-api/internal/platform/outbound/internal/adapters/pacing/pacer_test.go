package pacing_test

import (
	"altune/go-api/internal/platform/outbound/internal/adapters/pacing"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/resilience"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const shortDeadline = 50 * time.Millisecond

func newPacer(t testing.TB) *pacing.Pacer {
	t.Helper()
	p, err := pacing.New(time.Now, clock.Sleep)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPacer_RefusesAMissingClock(t *testing.T) {
	p, err := pacing.New(nil, clock.Sleep)

	if p != nil || !errors.Is(err, pacing.ErrClockRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrClockRequired", p, err)
	}
}

func TestPacer_RefusesAMissingSleeper(t *testing.T) {
	p, err := pacing.New(time.Now, nil)

	if p != nil || !errors.Is(err, pacing.ErrSleeperRequired) || !strings.Contains(err.Error(), "pacing.New") {
		t.Fatalf("got (%v, %v), want a refusal naming pacing.New wrapping ErrSleeperRequired", p, err)
	}
}

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
	assertBurstThenRefusal(t, newPacer(t), "musicbrainz.org", 1)
}

func TestPacer_OtherProvidersAllowABurstOfFour(t *testing.T) {
	assertBurstThenRefusal(t, newPacer(t), "api.deezer.com", 4)
}

func TestPacer_LeavesUnlistedHostsUnpaced(t *testing.T) {
	p := newPacer(t)
	for i, err := range waitsWithin(t, p, "unlisted.example.com", 100) {
		if err != nil {
			t.Fatalf("request %d to an unlisted host waited: %v", i+1, err)
		}
	}
}

func TestPacer_KeepsEachHostsBudgetApart(t *testing.T) {
	p := newPacer(t)
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

	if err := newPacer(t).Wait(req); err == nil {
		t.Fatal("a cancelled request to a paced host was let through")
	}
}

type paced struct {
	err     error
	elapsed time.Duration
}

func waitOn(ctx context.Context, t *testing.T, p *pacing.Pacer, host string) paced {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = p.Wait(req)
	return paced{err: err, elapsed: time.Since(start)}
}

func waitTogether(t *testing.T, p *pacing.Pacer, host string, callers int) []paced {
	t.Helper()
	results := make([]paced, callers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() { results[i] = waitOn(context.Background(), t, p, host) })
	}
	wg.Wait()
	return results
}

func assertQueueTimeout(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, resilience.ErrQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a queue timeout that is also a DeadlineExceeded", err)
	}
}

func TestPacer_QueuesUpToThreeIntervalsThenFailsFastWithTheQueueTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const callers = 6
		p := newPacer(t)

		results := waitTogether(t, p, "musicbrainz.org", callers)

		var admittedAt []time.Duration
		shed := 0
		for _, r := range results {
			if r.err == nil {
				admittedAt = append(admittedAt, r.elapsed)
				continue
			}
			assertQueueTimeout(t, r.err)
			if r.elapsed != 0 {
				t.Errorf("a shed caller waited %v, want it refused at once", r.elapsed)
			}
			shed++
		}
		slices.Sort(admittedAt)
		wantAdmitted := []time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second}
		if !slices.Equal(admittedAt, wantAdmitted) {
			t.Errorf("admitted after %v, want the burst of 1 then a queue of 3 spaced 1s apart: %v", admittedAt, wantAdmitted)
		}
		if shed != callers-len(wantAdmitted) {
			t.Errorf("shed %d callers, want %d", shed, callers-len(wantAdmitted))
		}
		if next := waitOn(context.Background(), t, p, "musicbrainz.org"); next.err != nil || next.elapsed != time.Second {
			t.Errorf("the next caller waited %v (err %v), want 1s because the shed callers kept no slot", next.elapsed, next.err)
		}
	})
}

func TestPacer_TwoShedCallersBackToBackKeepNoSlotSoTheNextCallerWaitsOneInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		const host = "musicbrainz.org"
		var queued sync.WaitGroup
		for range 4 {
			queued.Go(func() { waitOn(context.Background(), t, p, host) })
		}
		synctest.Wait()

		first := waitOn(context.Background(), t, p, host)
		second := waitOn(context.Background(), t, p, host)
		queued.Wait()
		next := waitOn(context.Background(), t, p, host)

		assertQueueTimeout(t, first.err)
		assertQueueTimeout(t, second.err)
		if next.err != nil || next.elapsed != time.Second {
			t.Errorf("the next caller waited %v (err %v), want exactly 1s because neither shed caller kept a slot", next.elapsed, next.err)
		}
	})
}

func TestPacer_ARetryWaitIsNeverShedByTheQueueCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Go(func() {
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://musicbrainz.org/x", nil)
				if err != nil {
					t.Error(err)
					return
				}
				errs[i] = p.WaitRetry(req)
			})
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("retry caller %d: err = %v, want it to wait its turn past the %v cap", i, err, resilience.MaxQueueWait)
			}
		}
	})
}

func TestPacer_AHostWithoutABoundedQueueKeepsQueueingEveryCaller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const callers = 12
		p := newPacer(t)

		results := waitTogether(t, p, "krakenfiles.com", callers)

		for i, r := range results {
			if r.err != nil {
				t.Errorf("caller %d: err = %v, want every caller to wait its turn", i, r.err)
			}
		}
	})
}

func TestPacer_AnUnboundedHostsShortDeadlineIsNeverClassifiedAsAQueueTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		waitTogether(t, p, "krakenfiles.com", 4)
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()

		err := waitOn(ctx, t, p, "krakenfiles.com").err

		if err == nil || errors.Is(err, resilience.ErrQueueTimeout) {
			t.Fatalf("err = %v, want a plain failure the breaker still counts", err)
		}
	})
}

type pacedTransport struct{ pacer *pacing.Pacer }

func (p pacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := p.pacer.Wait(req); err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestPacer_AWaitPastTheQueueCapThroughAClientWithATimeoutIsAQueueTimeoutNotAClientTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &http.Client{Transport: pacedTransport{pacer: newPacer(t)}, Timeout: 10 * time.Second}
		get := func() (time.Duration, error) {
			start := time.Now()
			resp, err := client.Get("https://itunes.apple.com/search")
			if err == nil {
				_ = resp.Body.Close()
			}
			return time.Since(start), err
		}
		for range 4 {
			get()
		}
		queued := make(chan error, 2)
		for range 2 {
			go func() { _, err := get(); queued <- err }()
		}
		synctest.Wait()

		elapsed, err := get()

		assertQueueTimeout(t, err)
		if elapsed != 0 {
			t.Errorf("a 12s wait under a 10s client timeout waited %v before refusing, want it refused at once", elapsed)
		}
		for range 2 {
			if err := <-queued; err != nil {
				t.Errorf("a wait inside the client timeout failed: %v", err)
			}
		}
	})
}

func TestPacer_TheQueueCapNeverPromisesMoreThanTheLongestWaitAClientAllows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		waitTogether(t, p, "itunes.apple.com", 4)

		results := waitTogether(t, p, "itunes.apple.com", 5)

		var admittedAt []time.Duration
		for _, r := range results {
			if r.err == nil {
				admittedAt = append(admittedAt, r.elapsed)
				continue
			}
			assertQueueTimeout(t, r.err)
		}
		slices.Sort(admittedAt)
		if want := []time.Duration{4 * time.Second, resilience.MaxQueueWait}; !slices.Equal(admittedAt, want) {
			t.Errorf("admitted after %v, want %v: one request per 4s, none queued past %v", admittedAt, want, resilience.MaxQueueWait)
		}
	})
}

func TestPacer_HoldsEachHostToItsStrictestRate(t *testing.T) {
	cases := []struct {
		host  string
		burst int
		gap   time.Duration
	}{
		{"musicbrainz.org", 1, time.Second},
		{"api.discogs.com", 1, time.Second},
		{"itunes.apple.com", 4, 4 * time.Second},
		{"api.acoustid.org", 3, time.Second / 3},
		{"cdnt-preview.dzcdn.net", 1, time.Second},
		{"www.youtube.com", 1, 200 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := newPacer(t)
				for i := range tc.burst {
					if r := waitOn(context.Background(), t, p, tc.host); r.err != nil || r.elapsed != 0 {
						t.Fatalf("request %d of the burst waited %v (err %v), want none", i+1, r.elapsed, r.err)
					}
				}

				if r := waitOn(context.Background(), t, p, tc.host); r.err != nil || r.elapsed != tc.gap {
					t.Errorf("first request past the burst waited %v (err %v), want %v", r.elapsed, r.err, tc.gap)
				}
			})
		})
	}
}

func TestPacer_FailsFastWhenTheDeadlineIsShorterThanTheWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		waitOn(context.Background(), t, p, "musicbrainz.org")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second/2)
		defer cancel()

		r := waitOn(ctx, t, p, "musicbrainz.org")

		assertQueueTimeout(t, r.err)
		if r.elapsed != 0 {
			t.Errorf("waited %v before refusing, want 0", r.elapsed)
		}
	})
}

func TestPacer_WaitsWhenTheDeadlineCoversTheWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		waitOn(context.Background(), t, p, "musicbrainz.org")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		r := waitOn(ctx, t, p, "musicbrainz.org")

		if r.err != nil || r.elapsed != time.Second {
			t.Errorf("waited %v (err %v), want 1s and no error", r.elapsed, r.err)
		}
	})
}

func TestPacer_AnExpiredCallerIsADeadlineErrorNotAQueueTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := waitOn(ctx, t, newPacer(t), "musicbrainz.org").err

	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, resilience.ErrQueueTimeout) {
		t.Fatalf("err = %v, want a plain DeadlineExceeded", err)
	}
}

func TestPacer_ACancelledWaitIsCanceledNotAQueueTimeoutAndFreesItsSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newPacer(t)
		waitOn(context.Background(), t, p, "musicbrainz.org")
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(time.Second/2, cancel)

		r := waitOn(ctx, t, p, "musicbrainz.org")

		if !errors.Is(r.err, context.Canceled) || errors.Is(r.err, resilience.ErrQueueTimeout) || r.elapsed != time.Second/2 {
			t.Fatalf("err = %v after %v, want Canceled after 500ms", r.err, r.elapsed)
		}
		if next := waitOn(context.Background(), t, p, "musicbrainz.org"); next.err != nil || next.elapsed != time.Second/2 {
			t.Errorf("the next caller waited %v (err %v), want the 500ms left of the first interval, so the cancelled caller kept no slot", next.elapsed, next.err)
		}
	})
}

var notFetchedHosts = map[string]bool{"e-cdns-images.dzcdn.net": true, "www.youtube.com": true}

var adapterURLHost = regexp.MustCompile(`"https?://([a-zA-Z0-9.-]+)`)

var inMemoryFakeAdapterDirsThatNeverFetch = map[string]bool{"memory": true}

func adapterHosts(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "..", "search", "internal", "adapters")
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
	p := newPacer(t)
	for _, h := range hosts {
		burst := 4
		if h == "musicbrainz.org" || h == "api.discogs.com" {
			burst = 1
		}
		assertBurstThenRefusal(t, p, h, burst)
	}
}
