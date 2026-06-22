package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

// fileWritingSearcher writes a real file into the outDir it is given, so the
// DownloadStep's temp-dir lifecycle can be exercised end to end.
type fileWritingSearcher struct {
	writeFile bool
	err       error
	gotURL    string
	gotDir    string
}

func (s *fileWritingSearcher) Search(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
	return nil, nil
}

func (s *fileWritingSearcher) Download(_ context.Context, url, outDir string) (string, error) {
	s.gotURL = url
	s.gotDir = outDir
	if s.err != nil {
		return "", s.err
	}
	path := filepath.Join(outDir, "track.mp3")
	if s.writeFile {
		if err := os.WriteFile(path, []byte("audio-bytes"), 0o644); err != nil {
			return "", err
		}
	}
	return path, nil
}

func TestDownloadStep_Execute_Success(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	step := NewDownloadStep(searcher)
	ac := &AcquisitionContext{Selected: &Candidate{URL: "https://example.com/x"}}

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.TempPath == "" {
		t.Fatal("expected TempPath to be set")
	}
	if _, err := os.Stat(ac.TempPath); err != nil {
		t.Errorf("downloaded file should exist: %v", err)
	}
	if searcher.gotURL != "https://example.com/x" {
		t.Errorf("download URL = %q, want the selected candidate URL", searcher.gotURL)
	}
}

func TestDownloadStep_Execute_NoSelected(t *testing.T) {
	step := NewDownloadStep(&fileWritingSearcher{})
	if err := step.Execute(context.Background(), &AcquisitionContext{}); err == nil {
		t.Fatal("expected error when no candidate is selected")
	}
}

func TestDownloadStep_Execute_DownloadError_CleansTempDir(t *testing.T) {
	searcher := &fileWritingSearcher{err: errors.New("boom")}
	step := NewDownloadStep(searcher)
	ac := &AcquisitionContext{Selected: &Candidate{URL: "https://example.com/x"}}

	if err := step.Execute(context.Background(), ac); err == nil {
		t.Fatal("expected download error")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty on failure, got %q", ac.TempPath)
	}
	if searcher.gotDir != "" {
		if _, err := os.Stat(searcher.gotDir); !os.IsNotExist(err) {
			t.Errorf("temp dir %q should be removed after a download error", searcher.gotDir)
		}
	}
}

func TestDownloadStep_Rollback_RemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "track.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	step := NewDownloadStep(&fileWritingSearcher{})
	if err := step.Rollback(context.Background(), &AcquisitionContext{TempPath: file}); err != nil {
		t.Fatalf("Rollback error: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("temp file should be removed after rollback")
	}
}
