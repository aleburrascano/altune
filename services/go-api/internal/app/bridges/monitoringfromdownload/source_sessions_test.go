package monitoringfromdownload_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromdownload"
	"context"
	"testing"
	"time"
)

type fakeSessions struct {
	since     time.Time
	signedOut bool
}

func (f fakeSessions) YouTubeSignedOutSince(context.Context) (time.Time, bool) {
	return f.since, f.signedOut
}

func TestSourceSessions_PassesTheSignedOutMomentThrough(t *testing.T) {
	since := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)

	got, signedOut := monitoringfromdownload.NewSourceSessions(fakeSessions{since: since, signedOut: true}).YouTubeSignedOutSince(context.Background())

	if !signedOut || !got.Equal(since) {
		t.Errorf("YouTubeSignedOutSince = (%v, %v), want (%v, true)", got, signedOut, since)
	}
}
