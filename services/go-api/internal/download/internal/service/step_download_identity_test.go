package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const loggedCap = 5

type pendingLedger struct {
	contracttest.Rules
	track ports.TrackSnapshot
}

func (l *pendingLedger) Get(context.Context, ids.TrackId, ids.UserId) (*ports.TrackSnapshot, error) {
	track := l.track
	return &track, nil
}

func (l *pendingLedger) Complete(context.Context, ids.TrackId, ids.UserId, ports.ClaimNumber, ports.Completion) error {
	return nil
}

func (l *pendingLedger) Fail(context.Context, ids.TrackId, ids.UserId, ports.ClaimNumber, string) error {
	return nil
}

func (l *pendingLedger) RevertAt(context.Context, ports.TrackSnapshot) error { return nil }

func (l *pendingLedger) Requeue(context.Context, ids.TrackId) error {
	return nil
}

func (l *pendingLedger) AudioRefInUse(context.Context, string, ids.TrackId) (bool, error) {
	return false, nil
}

type topicUpload struct{}

func (topicUpload) Name() domain.SourceName { return "topic-upload" }

func (topicUpload) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{downloadtest.Candidate(
		"https://www.youtube.com/watch?v=boysdontcry",
		downloadtest.WithTitle("Boys Don't Cry"),
		downloadtest.WithChannel("The Cure - Topic"),
		downloadtest.WithCategories([]string{"Music"}),
	)}, nil
}

func (topicUpload) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func acquireIdentifiedAs(t *testing.T, results []ports.AcoustIDResult) {
	t.Helper()
	track := ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Boys Don't Cry", Artist: "The Cure", Album: "Three Imaginary Boys", Status: ports.TrackPending,
	}
	svc := service.MustNewAcquireTrackAudioService(&pendingLedger{track: track}, service.NewSourceRegistry(topicUpload{}), storedAudio{},
		scratchfiles.New(),
		resolving(resolvedTo("mbid-a"), verifiedBy(&identifier{match: ports.RecordingMatch{AcoustID: "ac-1", Results: results}})), noStages())

	_ = svc.Execute(context.Background(), ports.AcquisitionJob{UserID: track.UserId, TrackID: track.ID})
}

type loggedResult struct {
	Score      float64  `json:"score"`
	Recordings []string `json:"recordings"`
}

func loggedVerdictResults(t *testing.T, buf *logging.Capture) []loggedResult {
	t.Helper()
	lines := bufio.NewScanner(strings.NewReader(buf.String()))
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
	buf := logging.CaptureJSON(t, slog.LevelInfo)

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
	buf := logging.CaptureJSON(t, slog.LevelInfo)
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
