package providers

import (
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

func TestSoundCloudAdapter_Name(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	if got := adapter.Name(); got != domain.ProviderSoundCloud {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderSoundCloud)
	}
}

func TestSoundCloudAdapter_SupportedKinds(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	kinds := adapter.SupportedKinds()

	if !kinds[domain.ResultKindTrack] {
		t.Error("expected track to be supported")
	}
	if kinds[domain.ResultKindAlbum] {
		t.Error("expected album to NOT be supported")
	}
	if kinds[domain.ResultKindArtist] {
		t.Error("expected artist to NOT be supported")
	}
}

func TestSoundCloudAdapter_SearchTimeout(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	if got := adapter.SearchTimeout(); got != 5*time.Second {
		t.Errorf("SearchTimeout() = %v, want %v", got, 5*time.Second)
	}
}

func TestSoundCloudAdapter_Search_UnsupportedKinds(t *testing.T) {
	adapter := NewSoundCloudAdapter()
	results, err := adapter.Search(context.Background(), "test query", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for unsupported kinds, got %d", len(results))
	}
}

func TestSoundCloudAdapter_Search_ContextCancelled(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	adapter := NewSoundCloudAdapter()
	_, err := adapter.Search(ctx, "test", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestSoundCloudAdapter_Search_CapsConcurrentYtdlp(t *testing.T) {
	release := make(chan struct{})
	var running, peak atomic.Int32
	adapter := NewSoundCloudAdapter()
	adapter.dumpJSON = func(ctx context.Context, args []string) ([][]byte, string, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		return nil, "", nil
	}
	kinds := map[domain.ResultKind]bool{domain.ResultKindTrack: true}

	const calls = 10
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		go func() {
			_, err := adapter.Search(context.Background(), "q", kinds)
			errs <- err
		}()
	}

	shed := 0
	deadline := time.After(2 * time.Second)
	for shed < calls-maxConcurrentYtdlp {
		select {
		case err := <-errs:
			if !errors.Is(err, ErrYtdlpBusy) {
				t.Fatalf("unexpected early result: %v", err)
			}
			shed++
		case <-deadline:
			t.Fatalf("only %d calls shed promptly, want %d", shed, calls-maxConcurrentYtdlp)
		}
	}
	if got := peak.Load(); got > maxConcurrentYtdlp {
		t.Errorf("peak concurrency %d exceeds cap %d", got, maxConcurrentYtdlp)
	}

	close(release)
	for i := 0; i < maxConcurrentYtdlp; i++ {
		if err := <-errs; err != nil {
			t.Errorf("admitted call failed: %v", err)
		}
	}

	if _, err := adapter.Search(context.Background(), "q", kinds); err != nil {
		t.Errorf("search after slots freed: %v", err)
	}
}
