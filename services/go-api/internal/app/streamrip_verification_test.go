package app

import (
	"altune/go-api/internal/shared/config"
	"os"
	"path/filepath"
	"testing"
)

// TestWireCatalogProbesStreamripBinary reproduces the defect where the
// streamrip rip binary was stored without any startup probe: a missing binary
// never showed up in AcquisitionVerification and only failed deep inside a
// background Fetch. The wired scheduler must now report the probe result.
func TestWireCatalogProbesStreamripBinary(t *testing.T) {
	present := filepath.Join(t.TempDir(), "rip")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake rip: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "absent", "rip")

	tests := []struct {
		name     string
		services []string
		bin      string
		want     bool
	}{
		{name: "configured binary missing is degraded", services: []string{"tidal"}, bin: missing, want: false},
		{name: "configured binary present is armed", services: []string{"tidal"}, bin: present, want: true},
		{name: "no streamrip service is not degraded", services: nil, bin: missing, want: true},
		{name: "only unsupported services is not degraded", services: []string{"napster"}, bin: missing, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				cfg: &config.Config{
					MusicDir:          t.TempDir(),
					YtMusicEnabled:    true,
					StreamripServices: tt.services,
					StreamripBin:      tt.bin,
				},
				sem: make(chan struct{}, 1),
			}

			if _, err := a.wireCatalog(nil, nil, nil); err != nil {
				t.Fatalf("wireCatalog: %v", err)
			}
			if a.scheduler == nil {
				t.Fatal("scheduler not wired")
			}
			if got := a.scheduler.Status().Verification.Streamrip; got != tt.want {
				t.Errorf("Verification.Streamrip = %v, want %v", got, tt.want)
			}
		})
	}
}
