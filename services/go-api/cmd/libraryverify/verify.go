package main

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"

	catalogPorts "altune/go-api/internal/catalog/ports"
)

const (
	maxReportedCandidates = 5
	progressEvery         = 25
	scratchDirPrefix      = "libraryverify-"
)

var errNoStoredAudio = errors.New("track is ready but has no stored audio")

type libraryTrack struct {
	ID       string
	UserID   string
	Artist   string
	Title    string
	AudioRef string
	Identity boundIdentity
}

type trackResult struct {
	TrackID    string        `json:"track_id"`
	UserID     string        `json:"user_id"`
	Artist     string        `json:"artist"`
	Title      string        `json:"title"`
	AudioRef   string        `json:"audio_ref"`
	Identity   boundIdentity `json:"identity"`
	Verdict    verdict       `json:"verdict"`
	Basis      string        `json:"basis,omitempty"`
	TopScore   float64       `json:"top_score"`
	Candidates []candidate   `json:"acoustid_candidates,omitempty"`
	Error      string        `json:"error,omitempty"`
}

type audioIdentifier interface {
	Identify(ctx context.Context, filePath string, durationHint float64) (ports.RecordingMatch, error)
}

type verifier struct {
	store     catalogPorts.AudioStore
	identify  audioIdentifier
	lookups   recordingLookups
	minScore  float64
	progress  io.Writer
	completed atomic.Int64
}

func (v *verifier) verify(ctx context.Context, track libraryTrack) trackResult {
	result := trackResult{
		TrackID: track.ID, UserID: track.UserID, Artist: track.Artist, Title: track.Title,
		AudioRef: track.AudioRef, Identity: track.Identity,
	}
	assessed, err := v.assess(ctx, track)
	if err != nil {
		result.Verdict, result.Error = verdictFailed, err.Error()
		return result
	}
	result.Verdict, result.Basis, result.TopScore = assessed.Verdict, assessed.Basis, assessed.topScore()
	result.Candidates = assessed.Candidates[:min(len(assessed.Candidates), maxReportedCandidates)]
	return result
}

func (v *verifier) assess(ctx context.Context, track libraryTrack) (assessment, error) {
	if track.AudioRef == "" {
		return assessment{}, errNoStoredAudio
	}
	dir, err := os.MkdirTemp("", scratchDirPrefix)
	if err != nil {
		return assessment{}, fmt.Errorf("create scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "audio"+path.Ext(track.AudioRef))
	if err := v.download(ctx, track.AudioRef, file); err != nil {
		return assessment{}, err
	}
	match, err := v.identify.Identify(ctx, file, 0)
	if err != nil {
		return assessment{}, fmt.Errorf("fingerprint: %w", err)
	}
	return judge(ctx, track.Identity, confidentCandidates(match.Results, v.minScore), v.lookups)
}

func (v *verifier) download(ctx context.Context, ref, destination string) error {
	stream, _, err := v.store.Stream(ctx, ref)
	if err != nil {
		return fmt.Errorf("stream %q: %w", ref, err)
	}
	defer func() { _ = stream.Close() }()
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create %q: %w", destination, err)
	}
	if _, err := io.Copy(file, stream); err != nil {
		_ = file.Close()
		return fmt.Errorf("download %q: %w", ref, err)
	}
	return file.Close()
}

func (v *verifier) verifyAll(ctx context.Context, tracks []libraryTrack, concurrency int) []trackResult {
	results := make([]trackResult, len(tracks))
	slots := make(chan struct{}, max(concurrency, 1))
	var group sync.WaitGroup
	for i, track := range tracks {
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			results[i] = v.verify(ctx, track)
			v.reportProgress(len(tracks))
		}()
	}
	group.Wait()
	return results
}

func (v *verifier) reportProgress(total int) {
	done := v.completed.Add(1)
	if done%progressEvery == 0 || int(done) == total {
		_, _ = fmt.Fprintf(v.progress, "libraryverify: %d/%d tracks verified\n", done, total)
	}
}
