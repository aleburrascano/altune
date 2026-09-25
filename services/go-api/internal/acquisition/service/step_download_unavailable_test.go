package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"testing"
)

type sequenceFetcher struct {
	errs  []error
	calls int
}

func (f *sequenceFetcher) Fetch(_ context.Context, _ ports.AudioCandidate, _ string) (string, error) {
	err := f.errs[f.calls]
	f.calls++
	return "", err
}

func TestDownloadStep_EarlierUnavailableSurvivesADifferentLastFailure(t *testing.T) {
	fetcher := &sequenceFetcher{errs: []error{
		&ports.SourceUnavailableError{Source: "ytdlp", Err: errors.New("HTTP Error 429")},
		errors.New("exit 1"),
	}}
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "u1"}, {URL: "u2"}}}

	_, err := NewDownloadStep(fetcher).Execute(context.Background(), ac, afterSelect{})

	if code := failureCode(&StepError{Step: stepNameDownload, Err: err}); code != domain.FailureSourceUnavailable {
		t.Fatalf("failure code = %s, want %s", code, domain.FailureSourceUnavailable)
	}
}
