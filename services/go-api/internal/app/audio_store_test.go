package app

import (
	"altune/go-api/internal/shared/config"
	"strings"
	"testing"
)

// TestBuildAudioStoreFailsFastWhenMisconfigured reproduces the defect where a
// misconfigured audio store booted healthy and panicked on the first stream or
// delete with a nil store. Startup must instead fail fast, naming the missing
// configuration, and never hand a nil store to the catalog services.
func TestBuildAudioStoreFailsFastWhenMisconfigured(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.Config
		wantContains []string
	}{
		{
			name:         "no backend configured",
			cfg:          &config.Config{},
			wantContains: []string{"MUSIC_DIR", "OCI_S3_ENDPOINT"},
		},
		{
			name: "incomplete OCI S3 names the missing variable",
			cfg: &config.Config{
				OCIS3Endpoint:  "https://objectstorage.example.com",
				OCIS3AccessKey: "key",
				OCIS3SecretKey: "secret",
				// OCI_S3_BUCKET deliberately missing.
			},
			wantContains: []string{"OCI_S3_BUCKET", "incomplete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{cfg: tt.cfg}

			store, err := a.buildAudioStore()
			if err == nil {
				t.Fatalf("expected startup error for misconfigured audio store, got store=%v", store)
			}
			if store != nil {
				t.Fatalf("expected nil store alongside error, got %v", store)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err.Error(), want)
				}
			}
		})
	}
}

// TestBuildAudioStoreFilesystemSucceeds guards that the supported filesystem
// backend still resolves to a live store without error.
func TestBuildAudioStoreFilesystemSucceeds(t *testing.T) {
	a := &App{cfg: &config.Config{MusicDir: t.TempDir()}}

	store, err := a.buildAudioStore()
	if err != nil {
		t.Fatalf("filesystem audio store should resolve, got error: %v", err)
	}
	if store == nil {
		t.Fatal("expected a non-nil filesystem audio store")
	}
}
