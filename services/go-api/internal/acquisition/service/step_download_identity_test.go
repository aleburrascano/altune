package service_test

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
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
	track acquisition.TrackSnapshot
}

func (l *pendingLedger) Get(context.Context, shared.TrackId, shared.UserId) (*acquisition.TrackSnapshot, error) {
	track := l.track
	return &track, nil
}

func (l *pendingLedger) Complete(context.Context, shared.TrackId, shared.UserId, acquisition.Fence, acquisition.Completion) error {
	return nil
}

func (l *pendingLedger) Fail(context.Context, shared.TrackId, shared.UserId, acquisition.Fence, string) error {
	return nil
}

func (l *pendingLedger) RevertAt(context.Context, acquisition.TrackSnapshot) error { return nil }

func (l *pendingLedger) AudioRefInUse(context.Context, string, shared.TrackId) (bool, error) {
	return false, nil
}

type topicUpload struct{}

func (topicUpload) Name() acquisition.SourceName { return "topic-upload" }

func (topicUpload) Find(context.Context, acquisition.FindRequest) ([]acquisition.AudioCandidate, error) {
	return []acquisition.AudioCandidate{{
		Title: "Boys Don't Cry", URL: "https://www.youtube.com/watch?v=boysdontcry", Channel: "The Cure - Topic", Categories: []string{"Music"},
	}}, nil
}

func (topicUpload) Fetch(_ context.Context, _ acquisition.AudioCandidate, outDir string) (acquisition.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return acquisition.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func acquireIdentifiedAs(t *testing.T, results []acquisition.AcoustIDResult) {
	t.Helper()
	track := acquisition.TrackSnapshot{
		ID: shared.NewTrackId(), UserId: shared.NewUserId(uuid.New()),
		Title: "Boys Don't Cry", Artist: "The Cure", Album: "Three Imaginary Boys", Status: acquisition.TrackPending,
	}
	svc := acquisition.NewAcquireTrackAudioService(&pendingLedger{track: track}, acquisition.NewSourceRegistry(topicUpload{}), storedAudio{},
		acquisition.WithAcquireTempWorkspace(acquisition.NewTempWorkspace()),
		acquisition.WithRecordingResolver(resolvedTo("mbid-a")),
		acquisition.WithAudioIdentifier(&identifier{match: acquisition.RecordingMatch{AcoustID: "ac-1", Results: results}}))

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

	acquireIdentifiedAs(t, []acquisition.AcoustIDResult{
		{Score: 0.97, Recordings: []acquisition.LinkedRecording{{MBID: "mbid-a"}, {MBID: "mbid-b"}}},
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
	recordings := make([]acquisition.LinkedRecording, loggedCap+3)
	results := make([]acquisition.AcoustIDResult, loggedCap+3)
	for i := range results {
		results[i] = acquisition.AcoustIDResult{Score: 0.5, Recordings: recordings}
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
