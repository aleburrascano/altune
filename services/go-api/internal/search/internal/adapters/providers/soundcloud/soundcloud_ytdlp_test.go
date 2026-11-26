package soundcloud_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/soundcloud"
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestSoundCloudAdapter_Name(t *testing.T) {
	adapter := soundcloud.NewSoundCloudAdapter()
	if got := adapter.Name(); got != search.ProviderSoundCloud {
		t.Errorf("Name() = %v, want %v", got, search.ProviderSoundCloud)
	}
}

func TestSoundCloudAdapter_SupportedKinds(t *testing.T) {
	adapter := soundcloud.NewSoundCloudAdapter()
	kinds := adapter.SupportedKinds()

	if !kinds[search.ResultKindTrack] {
		t.Error("expected track to be supported")
	}
	if kinds[search.ResultKindAlbum] {
		t.Error("expected album to NOT be supported")
	}
	if kinds[search.ResultKindArtist] {
		t.Error("expected artist to NOT be supported")
	}
}

func TestSoundCloudAdapter_SearchTimeout(t *testing.T) {
	adapter := soundcloud.NewSoundCloudAdapter()
	if got := adapter.SearchTimeout(); got != 5*time.Second {
		t.Errorf("SearchTimeout() = %v, want %v", got, 5*time.Second)
	}
}

func TestSoundCloudAdapter_Search_UnsupportedKinds(t *testing.T) {
	adapter := soundcloud.NewSoundCloudAdapter()
	results, err := adapter.Search(context.Background(), "test query", map[search.ResultKind]bool{
		search.ResultKindAlbum: true,
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

	adapter := soundcloud.NewSoundCloudAdapter()
	_, err := adapter.Search(ctx, "test", map[search.ResultKind]bool{
		search.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestSoundCloudAdapter_Search_CapsConcurrentYtdlp(t *testing.T) {
	release := make(chan struct{})
	var running, peak atomic.Int32
	adapter := soundcloud.NewSoundCloudAdapterWithRunner(func(ctx context.Context, args []string) ([][]byte, string, error) {
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
	})
	kinds := map[search.ResultKind]bool{search.ResultKindTrack: true}

	const calls = 10
	const slots = 3
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		go func() {
			_, err := adapter.Search(context.Background(), "q", kinds)
			errs <- err
		}()
	}

	shed := 0
	deadline := time.After(2 * time.Second)
	for shed < calls-slots {
		select {
		case err := <-errs:
			if !errors.Is(err, soundcloud.ErrYtdlpBusy) {
				t.Fatalf("unexpected early result: %v", err)
			}
			shed++
		case <-deadline:
			t.Fatalf("only %d calls shed promptly, want %d", shed, calls-slots)
		}
	}
	if got := peak.Load(); got > slots {
		t.Errorf("peak concurrency %d exceeds cap %d", got, slots)
	}

	close(release)
	for i := 0; i < slots; i++ {
		if err := <-errs; err != nil {
			t.Errorf("admitted call failed: %v", err)
		}
	}

	if _, err := adapter.Search(context.Background(), "q", kinds); err != nil {
		t.Errorf("search after slots freed: %v", err)
	}
}

func TestSoundCloudAdapter_Search_YtdlpEntryFillsTheTypedDuration(t *testing.T) {
	adapter := soundcloud.NewSoundCloudAdapterWithRunner(func(context.Context, []string) ([][]byte, string, error) {
		return [][]byte{[]byte(`{"title":"Song","uploader":"Artist","duration":226.5,"webpage_url":"https://soundcloud.com/a/s","id":"1","playback_count":9}`)}, "", nil
	})

	results, err := adapter.Search(context.Background(), "q", map[search.ResultKind]bool{search.ResultKindTrack: true})
	if err != nil || len(results) != 1 {
		t.Fatalf("Search = %d results, err %v, want one", len(results), err)
	}

	if results[0].Duration != 226 {
		t.Errorf("Duration = %d, want 226", results[0].Duration)
	}
	if _, present := results[0].Extras["duration"]; present {
		t.Errorf("extras = %v, want no duration key", results[0].Extras)
	}
}
