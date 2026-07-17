package service

import (
	"altune/go-api/internal/acquisition/ports"

	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// queueProber returns canned durations in order, one per ProbeDuration call —
// simulating ffprobe reporting each downloaded candidate's true length. decodeErrs
// canned per ValidateDecodable call simulate the ffmpeg decode gate (nil = decodes).
type queueProber struct {
	durations  []float64
	calls      int
	decodeErrs []error
	decCalls   int
}

func (p *queueProber) ProbeDuration(_ context.Context, _ string) (float64, error) {
	d := p.durations[p.calls]
	p.calls++
	return d, nil
}

func (p *queueProber) ValidateDecodable(_ context.Context, _ string) error {
	var err error
	if p.decCalls < len(p.decodeErrs) {
		err = p.decodeErrs[p.decCalls]
	}
	p.decCalls++
	return err
}

func TestDurationWithinTolerance(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual float64
		want             bool
	}{
		{name: "exact", expected: 226, actual: 226, want: true},
		{name: "few seconds off", expected: 226, actual: 231, want: true},
		{name: "within fraction", expected: 300, actual: 318, want: true},
		{name: "14-minute mix", expected: 226, actual: 840, want: false},
		{name: "30s preview", expected: 226, actual: 30, want: false},
		{name: "short track uses absolute slack", expected: 40, actual: 52, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := durationWithinTolerance(tt.expected, tt.actual); got != tt.want {
				t.Errorf("durationWithinTolerance(%v, %v) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

// TestDownloadStep_VerifiesAndFallsBack reproduces the "How Sweet" fix: the
// top-ranked candidate downloads to a 14:00 file, fails verification against the
// 3:46 expected duration, is discarded, and the next candidate (correct length)
// is downloaded and accepted.
func TestDownloadStep_VerifiesAndFallsBack(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840, 227}} // bloated, then correct
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track: TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=bloated", Duration: 840},
			{URL: "https://youtube.com/watch?v=correct", Duration: 227},
		},
	}

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if prober.calls != 2 {
		t.Errorf("expected both candidates probed, got %d probe calls", prober.calls)
	}
	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=correct" {
		t.Fatalf("expected the correct-length candidate selected, got %+v", ac.Selected)
	}
	if ac.TempPath == "" {
		t.Error("expected TempPath set for the accepted candidate")
	}
}

func TestDownloadStep_AllCandidatesWrongDuration_Errors(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840}}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=bloated", Duration: 840}},
	}

	if err := step.Execute(context.Background(), ac); err == nil {
		t.Fatal("expected an error when no candidate matches the expected duration")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when all candidates are rejected, got %q", ac.TempPath)
	}
}

// Without an expected duration there is nothing to verify against, so the first
// candidate is accepted unverified (preserves prior behaviour for legacy tracks).
func TestDownloadStep_NoExpectedDuration_SkipsVerification(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{durations: []float64{840}}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "Unknown", Artist: "Artist", Duration: 0},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=whatever", Duration: 840}},
	}

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if prober.calls != 0 {
		t.Errorf("expected no probing when expected duration is unknown, got %d calls", prober.calls)
	}
	if ac.TempPath == "" {
		t.Error("expected the first candidate accepted unverified")
	}
}

// TestDownloadStep_RejectsUndecodableAudio reproduces the corrupt-m4a incident:
// the first candidate downloads with the right duration but its samples don't
// decode; it is discarded and the next (decodable) candidate is accepted.
func TestDownloadStep_RejectsUndecodableAudio(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{
		durations:  []float64{226, 226},
		decodeErrs: []error{errors.New("audio stream failed to decode"), nil},
	}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track: TrackRef{Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{
			{URL: "https://youtube.com/watch?v=corrupt", Duration: 226},
			{URL: "https://youtube.com/watch?v=good", Duration: 226},
		},
	}

	if err := step.Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(ac.TempPath))

	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=good" {
		t.Fatalf("expected corrupt candidate rejected and decodable one accepted, got %+v", ac.Selected)
	}
}

// TestDownloadStep_AllUndecodable_Errors: when every candidate's audio is corrupt,
// the step fails (leaving the track un-stored) rather than persisting garbage.
func TestDownloadStep_AllUndecodable_Errors(t *testing.T) {
	searcher := &fileWritingSearcher{writeFile: true}
	prober := &queueProber{
		durations:  []float64{226},
		decodeErrs: []error{errors.New("audio stream failed to decode")},
	}
	step := NewDownloadStep(searcher, WithDownloadProber(prober))

	ac := &AcquisitionContext{
		Track:  TrackRef{Title: "X", Artist: "Y", Duration: 226},
		Ranked: []ports.AudioCandidate{{URL: "https://youtube.com/watch?v=corrupt", Duration: 226}},
	}

	if err := step.Execute(context.Background(), ac); err == nil {
		t.Fatal("expected an error when the only candidate is undecodable")
	}
	if ac.TempPath != "" {
		t.Errorf("TempPath must stay empty when the candidate is rejected, got %q", ac.TempPath)
	}
}

func TestRankCandidates_TopicFirstThenOrdered(t *testing.T) {
	track := TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226}
	candidates := []ports.AudioCandidate{
		{Title: "How Sweet", Channel: "HALLYUSOUND", Duration: 227, URL: "other", Categories: []string{"Music"}, ViewCount: 1000},
		{Title: "How Sweet", Channel: "NewJeans - Topic", Duration: 840, URL: "topic", Categories: []string{"Music"}, ViewCount: 9_000_000},
	}

	ranked := rankCandidates(context.Background(), track, candidates)
	if len(ranked) != 2 {
		t.Fatalf("expected both candidates ranked, got %d", len(ranked))
	}
	if ranked[0].URL != "topic" {
		t.Errorf("expected the Topic candidate ranked first, got %q", ranked[0].URL)
	}
	if ranked[1].URL != "other" {
		t.Errorf("expected the non-Topic candidate ranked second, got %q", ranked[1].URL)
	}
}
