package service

import (
	"context"
	"fmt"
	"os"

	"altune/go-api/internal/catalog/ports"
)

type DownloadStep struct {
	searcher ports.AudioSearcher
}

func NewDownloadStep(searcher ports.AudioSearcher) *DownloadStep {
	return &DownloadStep{searcher: searcher}
}

func (s *DownloadStep) Name() string { return "download" }

func (s *DownloadStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	if ac.Selected == nil {
		return fmt.Errorf("no candidate selected")
	}

	tmpDir, err := os.MkdirTemp("", "altune-acquire-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	filePath, err := s.searcher.Download(ctx, ac.Selected.URL, tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("download: %w", err)
	}

	ac.TempPath = filePath
	return nil
}

func (s *DownloadStep) Rollback(_ context.Context, ac *AcquisitionContext) error {
	if ac.TempPath != "" {
		os.RemoveAll(ac.TempPath)
	}
	return nil
}
