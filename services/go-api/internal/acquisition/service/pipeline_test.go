package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"strings"
	"testing"
)

type mockStep struct {
	name         string
	executeErr   error
	executePanic any
	// cancelJob, when set, cancels the pipeline's own context from inside
	// Execute: the acquireTimeout firing or the scheduler shutting down
	// mid-pipeline.
	cancelJob      context.CancelFunc
	rollbackErr    error
	executed       bool
	rolledBack     bool
	rollbackCtxErr error
	executionLog   *[]string
}

func newMockStep(name string, executionLog *[]string) *mockStep {
	return &mockStep{name: name, executionLog: executionLog}
}

func (s *mockStep) Name() string { return s.name }

func (s *mockStep) Execute(_ context.Context, _ *AcquisitionContext) error {
	s.executed = true
	if s.executionLog != nil {
		*s.executionLog = append(*s.executionLog, "execute:"+s.name)
	}
	if s.cancelJob != nil {
		s.cancelJob()
	}
	if s.executePanic != nil {
		panic(s.executePanic)
	}
	return s.executeErr
}

func (s *mockStep) Rollback(ctx context.Context, _ *AcquisitionContext) error {
	s.rolledBack = true
	s.rollbackCtxErr = ctx.Err()
	if s.executionLog != nil {
		*s.executionLog = append(*s.executionLog, "rollback:"+s.name)
	}
	return s.rollbackErr
}

// mockStage adapts a mockStep into the typed slot of one pipeline stage.
type mockStage[In, Out any] struct{ *mockStep }

func (m mockStage[In, Out]) Execute(ctx context.Context, ac *AcquisitionContext, _ In) (Out, error) {
	var out Out
	return out, m.mockStep.Execute(ctx, ac)
}

var pipelineStageNames = []string{"search", "select", "download", "tag", "store", "update_track"}

// pipelineOf fills the pipeline's stages in order with steps. Stages past
// len(steps) get an unlogged pass-through mock named for the stage.
func pipelineOf(steps ...*mockStep) Pipeline {
	all := make([]*mockStep, len(pipelineStageNames))
	for i, name := range pipelineStageNames {
		if i < len(steps) {
			all[i] = steps[i]
		} else {
			all[i] = &mockStep{name: name}
		}
	}
	return Pipeline{
		search:      mockStage[pipelineStart, afterSearch]{all[0]},
		selectBest:  mockStage[afterSearch, afterSelect]{all[1]},
		download:    mockStage[afterSelect, afterDownload]{all[2]},
		tag:         mockStage[afterDownload, afterTag]{all[3]},
		store:       mockStage[afterTag, afterStore]{all[4]},
		updateTrack: mockStage[afterStore, afterUpdate]{all[5]},
	}
}

func TestRunPipeline(t *testing.T) {
	tests := []struct {
		name           string
		buildSteps     func(log *[]string) Pipeline
		wantErr        bool
		wantErrContain string
		wantLog        []string
	}{
		{
			name: "all steps succeed in order",
			buildSteps: func(log *[]string) Pipeline {
				return pipelineOf(
					newMockStep("search", log),
					newMockStep("select", log),
					newMockStep("download", log),
				)
			},
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
			},
		},
		{
			name: "step 3 fails triggers rollback of steps 1 and 2 in reverse",
			buildSteps: func(log *[]string) Pipeline {
				s1 := newMockStep("search", log)
				s2 := newMockStep("select", log)
				s3 := newMockStep("download", log)
				s3.executeErr = errors.New("download failed: connection reset")
				return pipelineOf(s1, s2, s3)
			},
			wantErr:        true,
			wantErrContain: "step download",
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
				"rollback:select",
				"rollback:search",
			},
		},
		{
			name: "all six stages succeed in order",
			buildSteps: func(log *[]string) Pipeline {
				steps := make([]*mockStep, len(pipelineStageNames))
				for i, name := range pipelineStageNames {
					steps[i] = newMockStep(name, log)
				}
				return pipelineOf(steps...)
			},
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
				"execute:tag",
				"execute:store",
				"execute:update_track",
			},
		},
		{
			name: "first step fails with no prior steps to rollback",
			buildSteps: func(log *[]string) Pipeline {
				s1 := newMockStep("search", log)
				s1.executeErr = errors.New("searcher unavailable")
				s2 := newMockStep("select", log)
				return pipelineOf(s1, s2)
			},
			wantErr:        true,
			wantErrContain: "step search",
			wantLog: []string{
				"execute:search",
			},
		},
		{
			name: "cancelled context triggers rollback of completed steps",
			buildSteps: func(log *[]string) Pipeline {
				return pipelineOf(
					newMockStep("search", log),
					newMockStep("select", log),
				)
			},
			wantErr:        true,
			wantErrContain: "pipeline cancelled",
			wantLog:        nil,
		},
		{
			name: "rollback error does not mask original step error",
			buildSteps: func(log *[]string) Pipeline {
				s1 := newMockStep("search", log)
				s1.rollbackErr = errors.New("rollback failed: file locked")
				s2 := newMockStep("select", log)
				s2.executeErr = errors.New("no candidates matched")
				return pipelineOf(s1, s2)
			},
			wantErr:        true,
			wantErrContain: "step select",
			wantLog: []string{
				"execute:search",
				"execute:select",
				"rollback:search",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "cancelled context triggers rollback of completed steps" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				var log []string
				steps := tt.buildSteps(&log)
				ac := &AcquisitionContext{}

				err := RunPipeline(ctx, steps, ac)
				if err == nil {
					t.Fatal("expected error for cancelled context, got nil")
				}
				if !strings.Contains(err.Error(), "pipeline cancelled") {
					t.Errorf("error = %q, want it to contain %q", err.Error(), "pipeline cancelled")
				}
				return
			}

			ctx := context.Background()
			var log []string
			steps := tt.buildSteps(&log)
			ac := &AcquisitionContext{}

			err := RunPipeline(ctx, steps, ac)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrContain)
				}
				if !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContain)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if tt.wantLog != nil {
				if len(log) != len(tt.wantLog) {
					t.Fatalf("execution log length = %d, want %d\ngot:  %v\nwant: %v",
						len(log), len(tt.wantLog), log, tt.wantLog)
				}
				for i, entry := range tt.wantLog {
					if log[i] != entry {
						t.Errorf("execution log[%d] = %q, want %q\nfull log: %v", i, log[i], entry, log)
					}
				}
			}
		})
	}
}

func TestRunPipeline_SecondStepFails_OnlyFirstRolledBack(t *testing.T) {
	var log []string
	s1 := newMockStep("search", &log)
	s2 := newMockStep("select", &log)
	s2.executeErr = errors.New("no match")
	s3 := newMockStep("download", &log)

	steps := pipelineOf(s1, s2, s3)
	ac := &AcquisitionContext{}

	err := RunPipeline(context.Background(), steps, ac)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if s1.executed != true {
		t.Error("step 1 should have been executed")
	}
	if s2.executed != true {
		t.Error("step 2 should have been executed (it fails during execute)")
	}
	if s3.executed {
		t.Error("step 3 should NOT have been executed")
	}

	if s1.rolledBack != true {
		t.Error("step 1 should have been rolled back")
	}
	if s2.rolledBack {
		t.Error("step 2 should NOT have been rolled back (it failed, was never completed)")
	}
	if s3.rolledBack {
		t.Error("step 3 should NOT have been rolled back (never executed)")
	}
}

// A rollback runs precisely when the job's context is already done: the
// acquireTimeout fired, or the scheduler is shutting down. Compensations that
// inherit that cancellation fail their first call, so the audio stays in object
// storage with no reaper and the track never reverts.
func TestRunPipeline_RollbackRunsOnALiveContextAfterTheJobIsCancelled(t *testing.T) {
	cancellations := []struct {
		name       string
		executeErr error
	}{
		{name: "the job deadline fires between stages"},
		{name: "a stage fails as the job is cancelled", executeErr: errors.New("acquisition timed out")},
	}

	for _, tc := range cancellations {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var log []string
			search := newMockStep("search", &log)
			selectBest := newMockStep("select", &log)
			download := newMockStep("download", &log)
			download.cancelJob = cancel
			download.executeErr = tc.executeErr

			err := RunPipeline(ctx, pipelineOf(search, selectBest, download), &AcquisitionContext{})
			if err == nil {
				t.Fatal("expected the cancelled pipeline to fail, got nil")
			}

			for _, step := range []*mockStep{search, selectBest} {
				if !step.rolledBack {
					t.Fatalf("step %q was not rolled back", step.name)
				}
				if step.rollbackCtxErr != nil {
					t.Errorf("step %q rolled back on a dead context (%v): its compensation cannot reach storage",
						step.name, step.rollbackCtxErr)
				}
			}
		})
	}
}

// TestRunPipeline_StepPanic_RollsBackAndReturnsStepError reproduces the defect
// where a panic mid-pipeline skipped rollback and propagated out, leaving the
// track stranded at Pending. RunPipeline must recover the panic, roll back the
// completed steps in reverse, and return it as a *StepError so acquire.go's
// normal failure path can mark the track Failed.
func TestRunPipeline_StepPanic_RollsBackAndReturnsStepError(t *testing.T) {
	var log []string
	s1 := newMockStep("search", &log)
	s2 := newMockStep("select", &log)
	s3 := newMockStep("download", &log)
	s3.executePanic = "boom: nil map write"

	steps := pipelineOf(s1, s2, s3)
	ac := &AcquisitionContext{}

	err := RunPipeline(context.Background(), steps, ac)

	if err == nil {
		t.Fatal("expected a returned error from a panicking step, got nil (panic propagated instead)")
	}

	var stepErr *StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("error = %T (%v), want *StepError", err, err)
	}
	if stepErr.Step != "download" {
		t.Errorf("StepError.Step = %q, want %q", stepErr.Step, "download")
	}

	if !s1.rolledBack {
		t.Error("step 1 (search) should have been rolled back after the panic")
	}
	if !s2.rolledBack {
		t.Error("step 2 (select) should have been rolled back after the panic")
	}
	if s3.rolledBack {
		t.Error("step 3 (download) should NOT have been rolled back (it panicked mid-execute, never completed)")
	}

	wantLog := []string{
		"execute:search",
		"execute:select",
		"execute:download",
		"rollback:select",
		"rollback:search",
	}
	if len(log) != len(wantLog) {
		t.Fatalf("execution log = %v, want %v", log, wantLog)
	}
	for i, entry := range wantLog {
		if log[i] != entry {
			t.Errorf("execution log[%d] = %q, want %q\nfull log: %v", i, log[i], entry, log)
		}
	}
}

func TestAcqStage_BuildSearchQueries(t *testing.T) {
	tests := []struct {
		name        string
		track       TrackRef
		wantQueries []string
		wantMin     int
	}{
		{
			name:  "basic track produces title+artist queries",
			track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd"},
			wantQueries: []string{
				"Blinding Lights The Weeknd",
				"Blinding Lights The Weeknd audio",
			},
			wantMin: 2,
		},
		{
			name:  "track with album adds album query",
			track: TrackRef{Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding"},
			wantQueries: []string{
				"Circles Post Malone",
				"Circles Post Malone Hollywood's Bleeding",
				"Circles Post Malone audio",
			},
			wantMin: 3,
		},
		{
			name:  "track with ISRC includes ISRC as first query",
			track: TrackRef{Title: "HUMBLE.", Artist: "Kendrick Lamar", ISRC: "USUM71700626"},
			wantQueries: []string{
				"USUM71700626",
				"HUMBLE. Kendrick Lamar",
			},
			wantMin: 3,
		},
		{
			name:    "empty album skips album query",
			track:   TrackRef{Title: "Song", Artist: "Artist", Album: ""},
			wantMin: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := queriesFor(tt.track)
			if len(queries) < tt.wantMin {
				t.Errorf("expected at least %d queries, got %d: %v", tt.wantMin, len(queries), queries)
			}
			for _, want := range tt.wantQueries {
				found := false
				for _, got := range queries {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected query %q in %v", want, queries)
				}
			}
			t.Logf("queries: %v", queries)
		})
	}
}

func TestAcqStage_IdentityScore(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		artist    string
		candidate string
		wantAbove float64
		wantBelow float64
	}{
		{
			name:      "exact artist+title match",
			title:     "Blinding Lights",
			artist:    "The Weeknd",
			candidate: "The Weeknd - Blinding Lights",
			wantAbove: 80,
		},
		{
			name:      "title only match is penalized",
			title:     "Die Hard",
			artist:    "Dr. Dre",
			candidate: "DIE HARD",
			wantBelow: 65,
		},
		{
			name:      "correct artist+title beats wrong artist",
			title:     "Die Hard",
			artist:    "Dr. Dre",
			candidate: "Dr. Dre - Die Hard",
			wantAbove: 80,
		},
		{
			name:      "unrelated candidate scores very low",
			title:     "HUMBLE.",
			artist:    "Kendrick Lamar",
			candidate: "Cooking Tutorial Episode 47",
			wantBelow: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := identityScore(tt.title, tt.artist, tt.candidate)
			if tt.wantAbove > 0 && score < tt.wantAbove {
				t.Errorf("identityScore = %.1f, want >= %.1f", score, tt.wantAbove)
			}
			if tt.wantBelow > 0 && score >= tt.wantBelow {
				t.Errorf("identityScore = %.1f, want < %.1f", score, tt.wantBelow)
			}
			t.Logf("identityScore(%q, %q, %q) = %.1f", tt.title, tt.artist, tt.candidate, score)
		})
	}
}

func TestAcqStage_ArtistMatchesChannel(t *testing.T) {
	tests := []struct {
		artist  string
		channel string
		want    bool
	}{
		{"The Weeknd", "The Weeknd - Topic", true},
		{"Kendrick Lamar", "Kendrick Lamar - Topic", true},
		{"Dr. Dre", "Kendrick Lamar - Topic", false},
		{"Post Malone", "Post Malone - Topic", true},
		{"Post Malone", "Mac Miller - Topic", false},
		{"The Weeknd", "TheWeekndVEVO", true},
		{"Bad Bunny", "RandomUploader", false},
		// An artist of pure punctuation normalizes to "", which is a substring
		// of every channel: the whole class must fail to match, not just "!!!".
		{"!!!", "Random - Topic", false},
		{"???", "TheWeekndVEVO", false},
		{"", "x", false},
	}

	for _, tt := range tests {
		t.Run(tt.artist+"_"+tt.channel, func(t *testing.T) {
			got := artistMatchesChannel(tt.artist, tt.channel)
			if got != tt.want {
				t.Errorf("artistMatchesChannel(%q, %q) = %v, want %v", tt.artist, tt.channel, got, tt.want)
			}
		})
	}
}

// A symbol-only artist must not be read as provenance: without a real artist to
// recognise in the channel, the identity gate is the only thing standing between
// the track and an unrelated recording.
func TestAcqStage_SymbolOnlyArtistDoesNotRescueUnrelatedCandidate(t *testing.T) {
	track := TrackRef{Title: "Must Be the Moon", Artist: "!!!", Duration: 253}
	candidates := []ports.AudioCandidate{{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     "yt:cooking",
		Channel: "CookingChannel",
	}}

	ranked, rejected := rankAndCollect(context.Background(), track, candidates)

	if len(ranked) != 0 {
		t.Fatalf("unrelated candidate must stay gated for a symbol-only artist: ranked=%v", ranked)
	}
	if len(rejected) != 1 || rejected[0].Stage != RejectionIdentity || rejected[0].URL != "yt:cooking" {
		t.Fatalf("expected one identity rejection for yt:cooking, got %v", rejected)
	}
}

func TestAcqStage_MetadataRank(t *testing.T) {
	topic := ports.AudioCandidate{
		Channel:    "Artist - Topic",
		Categories: []string{"Music"},
		Duration:   200,
		ViewCount:  10_000_000,
	}
	vevo := ports.AudioCandidate{
		Channel:    "ArtistVEVO",
		Categories: []string{"Music"},
		Duration:   203,
		ViewCount:  500_000_000,
	}
	random := ports.AudioCandidate{
		Channel:    "RandomUploader",
		Categories: []string{"Entertainment"},
		Duration:   600,
		ViewCount:  1_000,
	}

	topicRank := metadataRank(topic, 200, 500_000_000)
	vevoRank := metadataRank(vevo, 200, 500_000_000)
	randomRank := metadataRank(random, 200, 500_000_000)

	if topicRank < 0.5 {
		t.Errorf("topic rank (%.3f) unexpectedly low", topicRank)
	}
	if vevoRank < 0.5 {
		t.Errorf("vevo rank (%.3f) unexpectedly low", vevoRank)
	}
	if randomRank >= vevoRank {
		t.Errorf("random (%.3f) should score lower than vevo (%.3f)", randomRank, vevoRank)
	}

	t.Logf("topic=%.3f, vevo=%.3f, random=%.3f", topicRank, vevoRank, randomRank)
}

func TestAcqStage_BuildAudioRef(t *testing.T) {
	tests := []struct {
		name     string
		track    TrackRef
		tempPath string
		want     string
	}{
		{
			name:     "basic",
			track:    TrackRef{UserID: "user-1", Artist: "The Weeknd", Album: "After Hours", Title: "Blinding Lights"},
			tempPath: "/tmp/acquire/Blinding Lights.mp3",
			want:     "user-1/the weeknd/after hours/blinding lights.mp3",
		},
		{
			name:     "empty album uses unknown",
			track:    TrackRef{UserID: "user-1", Artist: "Drake", Album: "", Title: "God's Plan"},
			tempPath: "/tmp/acquire/God's Plan.mp3",
			want:     "user-1/drake/unknown album/gods plan.mp3",
		},
		{
			name:     "special chars stripped and normalized",
			track:    TrackRef{UserID: "user-1", Artist: "AC/DC", Album: "Back in Black", Title: "Thunderstruck"},
			tempPath: "/tmp/acquire/Thunderstruck.mp3",
			want:     "user-1/ac dc/back in black/thunderstruck.mp3",
		},
		{
			name:     "extension follows downloaded file",
			track:    TrackRef{UserID: "user-1", Artist: "Drake", Album: "Views", Title: "One Dance"},
			tempPath: "/tmp/acquire/One Dance.m4a",
			want:     "user-1/drake/views/one dance.m4a",
		},
		{
			name:     "no extension falls back to mp3",
			track:    TrackRef{UserID: "user-1", Artist: "Drake", Album: "Views", Title: "One Dance"},
			tempPath: "",
			want:     "user-1/drake/views/one dance.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAudioRef(tt.track, tt.tempPath)
			if got != tt.want {
				t.Errorf("BuildAudioRef = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAcqStage_FullSelectionTrace(t *testing.T) {
	track := TrackRef{
		Title:    "Blinding Lights",
		Artist:   "The Weeknd",
		Duration: 200,
		ISRC:     "USUM71922973",
	}

	candidates := []ports.AudioCandidate{
		{
			Title:      "The Weeknd - Blinding Lights (Official Video)",
			Channel:    "TheWeekndVEVO",
			Duration:   203,
			Categories: []string{"Music"},
			ViewCount:  3_000_000_000,
		},
		{
			Title:      "Blinding Lights",
			Channel:    "The Weeknd - Topic",
			Duration:   200,
			Categories: []string{"Music"},
			ViewCount:  50_000_000,
		},
		{
			Title:      "Blinding Lights (Piano Cover)",
			Channel:    "Piano Tutorials",
			Duration:   195,
			Categories: []string{"Music"},
			ViewCount:  5_000_000,
		},
		{
			Title:      "Random Podcast Episode",
			Channel:    "PodcastGuy",
			Duration:   3600,
			Categories: []string{"Education"},
			ViewCount:  100_000,
		},
	}

	queries := queriesFor(track)
	t.Logf("Stage 1 — Search queries: %v", queries)

	for _, c := range candidates {
		ident := identityScore(track.Title, track.Artist, c.Title)
		artMatch := artistMatchesChannel(track.Artist, c.Channel)
		t.Logf("Stage 2 — Identity: %q (ch: %q) → score=%.1f, artistMatch=%v, gate=%v",
			c.Title, c.Channel, ident, artMatch, ident >= identityMin)
	}

	for _, q := range queries {
		norm := textnorm.NormalizeForMatch(q)
		t.Logf("Stage 3 — Normalize query: %q → %q", q, norm)
	}

	selected := selectBest(track, candidates)
	if selected == nil {
		t.Fatal("expected a candidate to be selected")
	}
	t.Logf("Stage 4 — Selected: %q (channel: %q)", selected.Title, selected.Channel)

	if !strings.Contains(selected.Channel, "Topic") {
		t.Errorf("expected Topic channel selected, got %q", selected.Channel)
	}
}

func queriesFor(track TrackRef) []string {
	return ports.SearchQueries(ports.FindRequest{
		Title:  track.Title,
		Artist: track.Artist,
		Album:  track.Album,
		ISRC:   track.ISRC,
	})
}
