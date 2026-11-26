package service_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

const loggedCap = 5

type pendingLedger struct {
	track download.TrackSnapshot
}

func (l *pendingLedger) Get(context.Context, ids.TrackId, ids.UserId) (*download.TrackSnapshot, error) {
	track := l.track
	return &track, nil
}

func (l *pendingLedger) Complete(context.Context, ids.TrackId, ids.UserId, download.ClaimNumber, download.Completion) error {
	return nil
}

func (l *pendingLedger) Fail(context.Context, ids.TrackId, ids.UserId, download.ClaimNumber, string) error {
	return nil
}

func (l *pendingLedger) RevertAt(context.Context, download.TrackSnapshot) error { return nil }

func (l *pendingLedger) Requeue(context.Context, ids.TrackId) error {
	return nil
}

func (l *pendingLedger) AudioRefInUse(context.Context, string, ids.TrackId) (bool, error) {
	return false, nil
}

type topicUpload struct{}

func (topicUpload) Name() download.SourceName { return "topic-upload" }

func (topicUpload) Find(context.Context, download.FindRequest) ([]download.AudioCandidate, error) {
	return []download.AudioCandidate{{
		Title: "Boys Don't Cry", URL: "https://www.youtube.com/watch?v=boysdontcry", Channel: "The Cure - Topic", Categories: []string{"Music"},
	}}, nil
}

func (topicUpload) Fetch(_ context.Context, _ download.AudioCandidate, outDir string) (download.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return download.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func acquireIdentifiedAs(t *testing.T, results []ports.AcoustIDResult) {
	t.Helper()
	track := download.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Boys Don't Cry", Artist: "The Cure", Album: "Three Imaginary Boys", Status: download.TrackPending,
	}
	svc := download.MustNewAcquireTrackAudioService(&pendingLedger{track: track}, download.NewSourceRegistry(topicUpload{}), storedAudio{},
		download.NewTempWorkspace(),
		resolving(resolvedTo("mbid-a"), verifiedBy(&identifier{match: download.RecordingMatch{AcoustID: "ac-1", Results: results}})), noStages())

	_ = svc.Execute(context.Background(), track.UserId, track.ID)
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type loggedResult struct {
	Score      float64  `json:"score"`
	Recordings []string `json:"recordings"`
}

func loggedVerdictResults(t *testing.T, buf *bytes.Buffer) []loggedResult {
	t.Helper()
	lines := bufio.NewScanner(buf)
	for lines.Scan() {
		var record struct {
			Msg     string         `json:"msg"`
			Results []loggedResult `json:"results"`
		}
		if json.Unmarshal(lines.Bytes(), &record) == nil && record.Msg == "acquisition.audio_verdict" {
			return record.Results
		}
	}
	t.Fatalf("no acquisition.audio_verdict record in:\n%s", buf.String())
	return nil
}

func TestExecute_TheAudioVerdictLogCarriesTheAcoustIDResults(t *testing.T) {
	buf := captureLog(t)

	acquireIdentifiedAs(t, []ports.AcoustIDResult{
		{Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: "mbid-a"}, {MBID: "mbid-b"}}},
		{Score: 0.41},
	})

	logged := loggedVerdictResults(t, buf)
	if len(logged) != 2 || logged[0].Score != 0.97 || logged[1].Score != 0.41 {
		t.Fatalf("logged results = %+v, want scores 0.97 and 0.41", logged)
	}
	if got := logged[0].Recordings; len(got) != 2 || got[0] != "mbid-a" || got[1] != "mbid-b" {
		t.Fatalf("first recordings = %v, want [mbid-a mbid-b]", got)
	}
}

func TestExecute_TheAudioVerdictLogCapsTheResultsAndTheirRecordings(t *testing.T) {
	buf := captureLog(t)
	recordings := make([]ports.LinkedRecording, loggedCap+3)
	results := make([]ports.AcoustIDResult, loggedCap+3)
	for i := range results {
		results[i] = ports.AcoustIDResult{Score: 0.5, Recordings: recordings}
	}

	acquireIdentifiedAs(t, results)

	logged := loggedVerdictResults(t, buf)
	if len(logged) != loggedCap {
		t.Fatalf("logged %d results, want the cap %d", len(logged), loggedCap)
	}
	if got := len(logged[0].Recordings); got != loggedCap {
		t.Fatalf("logged %d recordings, want the cap %d", got, loggedCap)
	}
}
