package main

import (
	"altune/go-api/internal/acquisition/adapters/deezerpreview"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/audiojudge"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	catalogPorts "altune/go-api/internal/catalog/ports"
)

const (
	maxReportedCandidates = 5
	progressEvery         = 25
	checkpointEvery       = 10
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
	TrackID    string           `json:"track_id"`
	UserID     string           `json:"user_id"`
	Artist     string           `json:"artist"`
	Title      string           `json:"title"`
	AudioRef   string           `json:"audio_ref"`
	Identity   boundIdentity    `json:"identity"`
	Verdict    verdict          `json:"verdict"`
	Basis      string           `json:"basis,omitempty"`
	TopScore   float64          `json:"top_score"`
	Candidates []candidate      `json:"acoustid_candidates,omitempty"`
	Preview    *previewEvidence `json:"preview,omitempty"`
	Error      string           `json:"error,omitempty"`
}

type audioIdentifier interface {
	Identify(ctx context.Context, filePath string, durationHint float64) (ports.RecordingMatch, error)
}

type previewSource interface {
	Clip(ctx context.Context, target deezerpreview.Target) (deezerpreview.Clip, error)
}

type verifier struct {
	store     catalogPorts.AudioStore
	identify  audioIdentifier
	lookups   recordingLookups
	minScore  float64
	progress  io.Writer
	completed atomic.Int64

	previews   previewSource
	printer    deezerpreview.RawPrinter
	cut        previewCut
	provenMax  float64
	checkpoint func(finished []trackResult)
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
	result.Verdict, result.Basis, result.TopScore, result.Preview = assessed.Verdict, assessed.Basis, assessed.topScore(), assessed.Preview
	result.Candidates = assessed.Candidates[:min(len(assessed.Candidates), maxReportedCandidates)]
	return result
}

func (v *verifier) withAudio(ctx context.Context, track libraryTrack, use func(file string) error) error {
	if track.AudioRef == "" {
		return errNoStoredAudio
	}
	dir, err := os.MkdirTemp("", scratchDirPrefix)
	if err != nil {
		return fmt.Errorf("create scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "audio"+path.Ext(track.AudioRef))
	if err := v.download(ctx, track.AudioRef, file); err != nil {
		return err
	}
	return use(file)
}

func (v *verifier) assess(ctx context.Context, track libraryTrack) (assessment, error) {
	var assessed assessment
	err := v.withAudio(ctx, track, func(file string) error {
		var err error
		assessed, err = v.assessFile(ctx, track, file)
		return err
	})
	return assessed, err
}

func (v *verifier) assessFile(ctx context.Context, track libraryTrack, file string) (assessment, error) {
	match, err := v.identify.Identify(ctx, file, 0)
	if err != nil {
		return assessment{}, fmt.Errorf("fingerprint: %w", err)
	}
	j := judgement{track: track, results: confidentResults(match.Results, v.minScore), cuts: v.cuts()}
	judged, err := judge(ctx, j, confidentCandidates(match.Results, v.minScore), v.lookups)
	if err != nil || v.previews == nil || !needsPreviewCheck(track.Identity, judged.Verdict) {
		return judged, err
	}
	evidence, err := v.measurePreview(ctx, track.Identity, file)
	if err != nil {
		return assessment{}, fmt.Errorf("preview: %w", err)
	}
	return withPreview(j, judged, evidence), nil
}

func (v *verifier) cuts() audiojudge.Cuts {
	cuts := audiojudge.CutsWithProvenMax(v.provenMax)
	cuts.LikelyMax = max(v.cut.matchMax, v.provenMax)
	cuts.AbsentMin = v.cut.absentMin
	return cuts
}

func confidentResults(results []ports.AcoustIDResult, minScore float64) []ports.AcoustIDResult {
	return slices.DeleteFunc(slices.Clone(results), func(result ports.AcoustIDResult) bool { return result.Score < minScore })
}

func (v *verifier) measurePreview(ctx context.Context, identity boundIdentity, file string) (previewEvidence, error) {
	clip, err := v.previews.Clip(ctx, identity.previewTarget())
	if err != nil {
		return previewEvidence{}, err
	}
	evidence := previewEvidence{DeezerTrackID: clip.DeezerTrackID, DeezerTitle: clip.DeezerTitle, Unavailable: clip.Unavailable}
	if clip.Unavailable != "" {
		return evidence, nil
	}
	filePrint, err := v.printer.Print(ctx, file)
	if err != nil {
		return previewEvidence{}, fmt.Errorf("fingerprint the file: %w", err)
	}
	return withAlignment(evidence, filePrint, clip.Print)
}

func withAlignment(evidence previewEvidence, filePrint, previewPrint []uint32) (previewEvidence, error) {
	if reason := deezerpreview.Incomparable(filePrint, previewPrint); reason != "" {
		evidence.Unavailable = reason
		return evidence, nil
	}
	aligned, err := deezerpreview.BestAlignment(filePrint, previewPrint)
	if err != nil {
		return previewEvidence{}, err
	}
	evidence.Alignment = &alignmentEvidence{
		BitErrorRate: aligned.BitErrorRate, OffsetSeconds: aligned.OffsetSeconds(),
		OverlapSeconds: deezerpreview.OverlapSeconds(len(previewPrint)),
	}
	return evidence, nil
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
	finished := &finishedResults{flushEvery: checkpointEvery, flush: v.checkpoint}
	slots := make(chan struct{}, max(concurrency, 1))
	var group sync.WaitGroup
	for i, track := range tracks {
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			results[i] = v.verify(ctx, track)
			finished.add(results[i])
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

type finishedResults struct {
	mu         sync.Mutex
	results    []trackResult
	flushEvery int
	flush      func([]trackResult)
}

func (f *finishedResults) add(result trackResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, result)
	if f.flush != nil && len(f.results)%f.flushEvery == 0 {
		f.flush(slices.Clone(f.results))
	}
}
