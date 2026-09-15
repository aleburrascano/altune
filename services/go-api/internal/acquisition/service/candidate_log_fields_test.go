package service

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	logTestTrackID = "11111111-2222-3333-4444-555555555555"
	logTestSource  = "soundcloud"
)

// scriptedProber returns fixed probe/decode results for every call.
type scriptedProber struct {
	duration  float64
	probeErr  error
	decodeErr error
}

func (p scriptedProber) ProbeDuration(context.Context, string) (float64, error) {
	return p.duration, p.probeErr
}

func (p scriptedProber) ValidateDecodable(context.Context, string) error { return p.decodeErr }

func captureJSONLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// findLogRecord returns the first JSON record whose msg is want.
func findLogRecord(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == want {
			return rec
		}
	}
	t.Fatalf("log %q not emitted; got:\n%s", want, buf.String())
	return nil
}

func logTestCandidate() ports.AudioCandidate {
	return ports.AudioCandidate{
		URL:    "https://soundcloud.com/artist/song",
		Title:  "Artist - Song",
		Source: logTestSource,
	}
}

func logTestDownloadContext() *AcquisitionContext {
	return &AcquisitionContext{
		Track:  TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist", Duration: 200},
		Ranked: []ports.AudioCandidate{logTestCandidate()},
	}
}

// TestCandidateLogs_StampTrackIDAndSource pins that every candidate-level
// rejection/evaluation log line carries track_id and source, so a log query
// filtered on track_id explains why a track failed to acquire (#984).
func TestCandidateLogs_StampTrackIDAndSource(t *testing.T) {
	identityCtx := func(cluster []string) *AcquisitionContext {
		ac := logTestDownloadContext()
		ac.Track.Duration = 0
		ac.Identity = ports.RecordingIdentity{MBID: "mb-want", AcoustIDs: cluster}
		return ac
	}

	tests := []struct {
		name string
		msg  string
		run  func(ctx context.Context, t *testing.T)
	}{
		{
			name: "search exclusion",
			msg:  "acquisition.candidate_excluded",
			run: func(ctx context.Context, t *testing.T) {
				c := logTestCandidate()
				other := ports.AudioCandidate{URL: "https://soundcloud.com/artist/other", Source: logTestSource}
				step := NewSearchStep(&fakeAudioSearcher{searchResults: []ports.AudioCandidate{c, other}})
				ac := &AcquisitionContext{
					Track:   TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist"},
					Replace: ReplaceState{ExcludeKeys: []string{sourceKey(c.URL)}},
				}
				_, _ = step.Execute(ctx, ac, pipelineStart{})
			},
		},
		{
			name: "candidate evaluation",
			msg:  "candidate_evaluated",
			run: func(ctx context.Context, t *testing.T) {
				track := TrackRef{ID: logTestTrackID, Title: "Song", Artist: "Artist"}
				_, _ = rankAndCollect(ctx, track, []ports.AudioCandidate{logTestCandidate()})
			},
		},
		{
			name: "download failure",
			msg:  "acquisition.candidate_download_failed",
			run: func(ctx context.Context, t *testing.T) {
				step := NewDownloadStep(&fileWritingSearcher{err: errors.New("fetch boom")})
				_, _ = step.Execute(ctx, logTestDownloadContext(), afterSelect{})
			},
		},
		{
			name: "probe failure",
			msg:  "acquisition.probe_failed_accepting",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					WithDownloadProber(scriptedProber{probeErr: errors.New("probe boom")}))
			},
		},
		{
			name: "duration rejection",
			msg:  "acquisition.candidate_rejected_duration",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					WithDownloadProber(scriptedProber{duration: 30}))
			},
		},
		{
			name: "undecodable rejection",
			msg:  "acquisition.candidate_rejected_undecodable",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, logTestDownloadContext(),
					WithDownloadProber(scriptedProber{duration: 200, decodeErr: errors.New("decode boom")}))
			},
		},
		{
			name: "identify failure",
			msg:  "acquisition.identify_failed",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(nil),
					WithDownloadIdentifier(&stubIdentifier{err: errors.New("acoustid boom")}))
			},
		},
		{
			name: "identify unknown",
			msg:  "acquisition.identify_unknown",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(nil),
					WithDownloadIdentifier(&stubIdentifier{}))
			},
		},
		{
			name: "identify uncorroborated",
			msg:  "acquisition.identify_uncorroborated",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx(nil),
					WithDownloadIdentifier(&stubIdentifier{match: ports.RecordingMatch{MBIDs: []string{"mb-other"}}}))
			},
		},
		{
			name: "fingerprint rejection",
			msg:  "acquisition.candidate_rejected_fingerprint",
			run: func(ctx context.Context, t *testing.T) {
				runDownloadForLog(ctx, t, identityCtx([]string{"ac-want"}),
					WithDownloadIdentifier(&stubIdentifier{match: ports.RecordingMatch{AcoustID: "ac-other", MBIDs: []string{"mb-other"}}}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureJSONLog(t)
			tt.run(context.Background(), t)

			rec := findLogRecord(t, logs, tt.msg)
			if got := rec["track_id"]; got != logTestTrackID {
				t.Errorf("%s track_id = %v, want %q; record: %v", tt.msg, got, logTestTrackID, rec)
			}
			if got := rec["source"]; got != logTestSource {
				t.Errorf("%s source = %v, want %q; record: %v", tt.msg, got, logTestSource, rec)
			}
		})
	}
}

// runDownloadForLog runs the download step over ac and removes any accepted
// temp audio so fail-open scenarios do not leak scratch dirs.
func runDownloadForLog(ctx context.Context, t *testing.T, ac *AcquisitionContext, opts ...func(*DownloadStep)) {
	t.Helper()
	step := NewDownloadStep(&fileWritingSearcher{writeFile: true}, opts...)
	_, _ = step.Execute(ctx, ac, afterSelect{})
	if ac.TempPath != "" {
		_ = os.RemoveAll(filepath.Dir(ac.TempPath))
	}
}
