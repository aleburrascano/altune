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
	"strings"

	catalogPorts "altune/go-api/internal/catalog/ports"
)

var (
	errAudioChanged   = errors.New("audio stream changed while tagging")
	errTagsNotWritten = errors.New("file tags still differ from the database after tagging")
)

type verdict string

const (
	verdictMatched      verdict = "matched"
	verdictWouldRetag   verdict = "would-retag"
	verdictRetagged     verdict = "retagged"
	verdictMoved        verdict = "moved"
	verdictSharedRef    verdict = "shared-ref"
	verdictLimitReached verdict = "limit-reached"
	verdictFailed       verdict = "failed"
)

type trackRow struct {
	ID     string
	UserID string
	Ref    string
	Tags   ports.TrackTags
}

type outcome struct {
	Track    trackRow
	Verdict  verdict
	Diffs    []tagDiff
	Leftover string
	Err      error
}

type retagger struct {
	store       catalogPorts.AudioStore
	tagger      ports.AudioTagger
	media       mediaTools
	attemptID   func() string
	stillServes func(ctx context.Context, track trackRow) (bool, error)
	claim       func() bool
}

func stagedRef(ref, attemptID string) string {
	ext := path.Ext(ref)
	return strings.TrimSuffix(ref, ext) + ".retag-" + attemptID + ext
}

func (r retagger) download(ctx context.Context, ref, destination string) error {
	stream, _, err := r.store.Stream(ctx, ref)
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

func (r retagger) inspect(ctx context.Context, track trackRow, dir string) (original string, diffs []tagDiff, err error) {
	original = filepath.Join(dir, "original"+path.Ext(track.Ref))
	if err := r.download(ctx, track.Ref, original); err != nil {
		return "", nil, err
	}
	current, err := r.media.ReadTags(ctx, original)
	if err != nil {
		return "", nil, err
	}
	return original, diffTags(track.Tags, current), nil
}

func (r retagger) process(ctx context.Context, track trackRow, apply bool) outcome {
	dir, err := os.MkdirTemp("", "retag-")
	if err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: err}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	original, diffs, err := r.inspect(ctx, track, dir)
	switch {
	case err != nil:
		return outcome{Track: track, Verdict: verdictFailed, Err: err}
	case len(diffs) == 0:
		return outcome{Track: track, Verdict: verdictMatched}
	case !r.claim():
		return outcome{Track: track, Verdict: verdictLimitReached}
	case !apply:
		return outcome{Track: track, Verdict: verdictWouldRetag, Diffs: diffs}
	}
	result := r.replace(ctx, track, dir, original)
	result.Diffs = diffs
	return result
}

func (r retagger) replace(ctx context.Context, track trackRow, dir, original string) outcome {
	digest, err := r.media.AudioDigest(ctx, original)
	if err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: err}
	}
	if err := r.tagger.Tag(ctx, original, track.Tags); err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: fmt.Errorf("tag: %w", err)}
	}
	if err := r.verifyFile(ctx, original, track.Tags, digest); err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: err}
	}

	staged := stagedRef(track.Ref, r.attemptID())
	if err := r.store.Store(ctx, original, staged); err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: fmt.Errorf("stage upload: %w", err)}
	}
	result := r.promote(ctx, track, dir, staged, digest)
	if err := r.store.Delete(ctx, staged); err != nil {
		result.Leftover = staged
	}
	return result
}

func (r retagger) promote(ctx context.Context, track trackRow, dir, staged, digest string) outcome {
	roundTrip := filepath.Join(dir, "staged"+path.Ext(staged))
	if err := r.download(ctx, staged, roundTrip); err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: err}
	}
	if err := r.verifyFile(ctx, roundTrip, track.Tags, digest); err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: fmt.Errorf("staged object: %w", err)}
	}
	serves, err := r.stillServes(ctx, track)
	if err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: fmt.Errorf("recheck track: %w", err)}
	}
	if !serves {
		return outcome{Track: track, Verdict: verdictMoved}
	}
	if err := r.store.Store(ctx, roundTrip, track.Ref); err != nil {
		return outcome{Track: track, Verdict: verdictFailed, Err: fmt.Errorf("replace object: %w", err)}
	}
	return outcome{Track: track, Verdict: verdictRetagged}
}

func (r retagger) verifyFile(ctx context.Context, file string, want ports.TrackTags, wantDigest string) error {
	digest, err := r.media.AudioDigest(ctx, file)
	if err != nil {
		return err
	}
	if digest != wantDigest {
		return errAudioChanged
	}
	got, err := r.media.ReadTags(ctx, file)
	if err != nil {
		return err
	}
	if diffs := diffTags(want, got); len(diffs) > 0 {
		return fmt.Errorf("%w: %s", errTagsNotWritten, describeDiffs(diffs))
	}
	return nil
}

func describeDiffs(diffs []tagDiff) string {
	parts := make([]string, 0, len(diffs))
	for _, d := range diffs {
		parts = append(parts, fmt.Sprintf("%s %q -> %q", d.Field, d.Got, d.Want))
	}
	return strings.Join(parts, "; ")
}
