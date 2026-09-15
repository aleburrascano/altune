package app

import (
	"altune/go-api/internal/shared/config"
	"testing"
)

// TestWireCatalogSchedulerGating pins wireCatalog's observable object graph so
// splitting it into named wiring steps stays behavior-preserving: with no
// acquisition source the scheduler and the retry/reacquire handlers must stay
// nil, and with a source they must all be built and the scheduler recorded on
// the App for shutdown.
func TestWireCatalogSchedulerGating(t *testing.T) {
	tests := []struct {
		name          string
		ytMusic       bool
		wantScheduler bool
	}{
		{name: "no sources leaves acquisition unwired", ytMusic: false, wantScheduler: false},
		{name: "a source wires scheduler and retry handlers", ytMusic: true, wantScheduler: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				cfg: &config.Config{MusicDir: t.TempDir(), YtMusicEnabled: tt.ytMusic},
				sem: make(chan struct{}, 1),
			}

			got, err := a.wireCatalog(nil, nil, nil)
			if err != nil {
				t.Fatalf("wireCatalog: %v", err)
			}

			assertCatalogHandlersBuilt(t, got)
			if (a.scheduler != nil) != tt.wantScheduler {
				t.Errorf("a.scheduler set = %v, want %v", a.scheduler != nil, tt.wantScheduler)
			}
			if (got.retryH != nil) != tt.wantScheduler {
				t.Errorf("retryH set = %v, want %v", got.retryH != nil, tt.wantScheduler)
			}
			if (got.reacquireH != nil) != tt.wantScheduler {
				t.Errorf("reacquireH set = %v, want %v", got.reacquireH != nil, tt.wantScheduler)
			}
		})
	}
}

func TestWireCatalogFailsWithoutAudioStore(t *testing.T) {
	a := &App{cfg: &config.Config{YtMusicEnabled: true}}

	if _, err := a.wireCatalog(nil, nil, nil); err == nil {
		t.Fatal("wireCatalog with no audio store config: want error, got nil")
	}
	if a.scheduler != nil {
		t.Error("scheduler must not be wired when the audio store fails")
	}
}

func assertCatalogHandlersBuilt(t *testing.T, w catalogWiring) {
	t.Helper()
	if w.trackRepo == nil || w.setTrackNumberSvc == nil {
		t.Error("trackRepo and setTrackNumberSvc must be wired")
	}
	if w.trackHandler == nil || w.libraryHandler == nil || w.playlistHandler == nil {
		t.Error("track, library and playlist handlers must be wired")
	}
	if w.streamHandler == nil || w.audioURLHandler == nil {
		t.Error("stream and audio URL handlers must be wired")
	}
}
