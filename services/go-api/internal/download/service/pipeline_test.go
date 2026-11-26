package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/sourcekey"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func coreStepsOver(sources *service.SourceRegistry, store *objectStore, workspace ports.ScratchFolder) service.Pipeline {
	steps, err := service.CoreSteps(sources, ports.NoopAudioTagger(), store, ports.NoopAudioProber(),
		ports.NoopAudioIdentifier(), ports.NoopRecordingLookup(), ports.NoopVerifySkipRecorder(),
		service.MustDirectDeleteOrphans(store), ports.NoopAudioRefLookup(), clock.Sleep, random.Real, time.Now, workspace, service.Unlimited(), nil)
	if err != nil {
		panic(err)
	}
	return steps
}

func TestSelect_SymbolOnlyArtistDoesNotRescueUnrelatedCandidate(t *testing.T) {
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     "yt:cooking",
		Channel: "CookingChannel",
	}}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Must Be the Moon", Artist: "!!!", CatalogDuration: 253}}

	err := runPipeline(context.Background(), service.NewSourceRegistry(source), ac)

	if step := failedStep(t, err).Step; step != "select" {
		t.Fatalf("err = %v, want the unrelated candidate to stay gated at select", err)
	}
	if len(ac.Rejections) != 1 || ac.Rejections[0].Stage != rejectedcandidates.Identity || ac.Rejections[0].URL != "yt:cooking" {
		t.Fatalf("rejections = %v, want one identity rejection for yt:cooking", ac.Rejections)
	}
}

func TestBuildAudioRef(t *testing.T) {
	tests := []struct {
		name     string
		track    service.TrackRef
		tempPath string
		want     string
	}{
		{
			name:     "basic",
			track:    service.TrackRef{UserID: "user-1", Artist: "The Weeknd", Album: "After Hours", Title: "Blinding Lights"},
			tempPath: "/tmp/acquire/Blinding Lights.mp3",
			want:     "user-1/the weeknd/after hours/blinding lights.mp3",
		},
		{
			name:     "empty album uses unknown",
			track:    service.TrackRef{UserID: "user-1", Artist: "Drake", Album: "", Title: "God's Plan"},
			tempPath: "/tmp/acquire/God's Plan.mp3",
			want:     "user-1/drake/unknown album/gods plan.mp3",
		},
		{
			name:     "special chars stripped and normalized",
			track:    service.TrackRef{UserID: "user-1", Artist: "AC/DC", Album: "Back in Black", Title: "Thunderstruck"},
			tempPath: "/tmp/acquire/Thunderstruck.mp3",
			want:     "user-1/ac dc/back in black/thunderstruck.mp3",
		},
		{
			name:     "extension follows downloaded file",
			track:    service.TrackRef{UserID: "user-1", Artist: "Drake", Album: "Views", Title: "One Dance"},
			tempPath: "/tmp/acquire/One Dance.m4a",
			want:     "user-1/drake/views/one dance.m4a",
		},
		{
			name:     "no extension falls back to mp3",
			track:    service.TrackRef{UserID: "user-1", Artist: "Drake", Album: "Views", Title: "One Dance"},
			tempPath: "",
			want:     "user-1/drake/views/one dance.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.BuildAudioRef(tt.track, tt.tempPath); got != tt.want {
				t.Errorf("BuildAudioRef = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelect_PrefersTheTopicUploadOverCoversAndUnrelatedVideos(t *testing.T) {
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{
		{
			Title: "The Weeknd - Blinding Lights (Official Video)", Channel: "TheWeekndVEVO",
			Duration: 203, Categories: []string{"Music"}, ViewCount: 3_000_000_000, URL: "yt:official",
		},
		{
			Title: "Blinding Lights", Channel: "The Weeknd - Topic",
			Duration: 200, Categories: []string{"Music"}, ViewCount: 50_000_000, URL: "yt:topic",
		},
		{
			Title: "Blinding Lights (Piano Cover)", Channel: "Piano Tutorials",
			Duration: 195, Categories: []string{"Music"}, ViewCount: 5_000_000, URL: "yt:cover",
		},
		{
			Title: "Random Podcast Episode", Channel: "PodcastGuy",
			Duration: 3600, Categories: []string{"Education"}, ViewCount: 100_000, URL: "yt:podcast",
		},
	}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{
		Title: "Blinding Lights", Artist: "The Weeknd", CatalogDuration: 200, ISRC: "USUM71922973",
	}}

	_ = runPipeline(context.Background(), service.NewSourceRegistry(source), ac)

	if ac.Selected == nil || ac.Selected.URL != "yt:topic" {
		t.Fatalf("Selected = %+v, want the Topic channel upload", ac.Selected)
	}
}

func TestProvenance(t *testing.T) {
	tests := []struct {
		name string
		ac   service.AcquisitionContext
		want downloadprovenance.AcquisitionProvenance
	}{
		{
			name: "fingerprint match is the strongest claim",
			ac: service.AcquisitionContext{
				IdentityVerified: true,
				DurationVerified: true,
				Identity:         ports.RecordingIdentity{Duration: 181},
			},
			want: downloadprovenance.ProvenanceVerified,
		},
		{
			name: "length corroborated by discovery but no fingerprint",
			ac: service.AcquisitionContext{
				DurationVerified: true,
				Track:            service.TrackRef{CatalogDuration: 181},
				Identity:         ports.RecordingIdentity{Duration: 181},
			},
			want: downloadprovenance.ProvenanceCorroborated,
		},
		{
			name: "length checked against saved metadata only",
			ac:   service.AcquisitionContext{DurationVerified: true},
			want: downloadprovenance.ProvenanceBestEffort,
		},
		{
			name: "nothing verified",
			ac:   service.AcquisitionContext{},
			want: downloadprovenance.ProvenanceBestEffort,
		},
		{
			name: "a best-effort settlement outranks a fingerprint match",
			ac:   service.AcquisitionContext{BestEffort: true, IdentityVerified: true},
			want: downloadprovenance.ProvenanceBestEffort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ac.Provenance(); got != tt.want {
				t.Errorf("Provenance() = %q, want %q", got, tt.want)
			}
		})
	}
}

type tierSource struct {
	name   acqdomain.SourceName
	url    string
	broken bool
}

func (s tierSource) Name() acqdomain.SourceName { return s.name }

func (s tierSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return []ports.AudioCandidate{{
		Title: "Blinding Lights", URL: s.url, Channel: "The Weeknd - Topic", Artists: []string{"The Weeknd"},
		Duration: 200, Categories: []string{"Music"}, Kind: ports.KindArtTrack,
	}}, nil
}

func (s tierSource) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if s.broken {
		return ports.FetchedAudio{}, errors.New("yt-dlp: download failed")
	}
	path := filepath.Join(outDir, "track.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("audio-bytes"), 0o600)
}

func TestRunPipeline_FallbackWindowsContinueTheFirstTiersCount(t *testing.T) {
	const firstTier, fallback = "https://example.com/first-tier", "https://example.com/fallback"
	registry := service.NewSourceRegistry(tierSource{name: "first", url: firstTier, broken: true}).
		WithFallback(tierSource{name: "fallback", url: fallback})
	ac := &service.AcquisitionContext{Track: service.TrackRef{ID: "t1", Title: "Blinding Lights", Artist: "The Weeknd", CatalogDuration: 200}}
	workspace := scratchfiles.New()
	steps := coreStepsOver(registry, &objectStore{objects: map[string]string{}}, workspace)

	if err := service.RunPipeline(context.Background(), steps, ac); err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	service.CleanupTemp(context.Background(), workspace, ac)

	want := []service.AttemptedCandidate{{URL: firstTier, Window: 0}, {URL: fallback, Window: 1}}
	if !slices.Equal(ac.Attempted, want) {
		t.Errorf("Attempted = %+v, want %+v: the fallback tier's windows continue the first tier's numbering", ac.Attempted, want)
	}
}

func TestRunPipeline_PlainFailureDeletesTheStoredAudioAndQueuesNothing(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ledger, store, queue := acquisitiontest.NewLedger(track), newRecordingStore(), &recordingOrphanQueue{}
	ledger.FailWrites(errors.New("database unavailable"))

	err := storing{t, ledger, store, track}.executeStaged(noStages(orphaningInto(queue)))

	if err == nil {
		t.Fatal("Execute succeeded, want the failed commit")
	}
	if len(store.writes) != 1 || len(store.objects) != 0 {
		t.Errorf("writes = %v, objects = %v, want the audio stored once and deleted", store.writes, store.objects)
	}
	if recorded := queue.entries(); len(recorded) != 0 {
		t.Errorf("recorded = %+v, want nothing queued", recorded)
	}
}

type waitingCommitLedger struct{ *acquisitiontest.Ledger }

func (waitingCommitLedger) Complete(ctx context.Context, _ ids.TrackId, _ ids.UserId, _ ports.ClaimNumber, _ ports.Completion) error {
	<-ctx.Done()
	return ctx.Err()
}

type lostLeaseQueue struct{ *firstOutcomeQueue }

func (lostLeaseQueue) Heartbeat(context.Context, ids.TrackId, ports.ClaimNumber, time.Duration) error {
	return acqdomain.ErrLeaseLost
}

type storedAndOrphaned struct {
	store  *recordingStore
	queued []ports.OrphanedAudio
	track  acquisitiontest.LedgerTrack
}

func runJobWaitingOnItsCommit(t *testing.T, leaseLost bool) storedAndOrphaned {
	t.Helper()
	var got storedAndOrphaned
	synctest.Test(t, func(t *testing.T) {
		track := pending("Song", "Artist", "Album")
		store, orphans := newRecordingStore(), &recordingOrphanQueue{}
		svc := service.MustNewAcquireTrackAudioService(waitingCommitLedger{acquisitiontest.NewLedger(track)}, sourceFor(track), store,
			scratchfiles.New(), noVerification(), noStages(orphaningInto(orphans)))
		opts := []any{}
		queue := &firstOutcomeQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), outcome: make(chan jobOutcome, 1)}
		if leaseLost {
			opts = append(opts, withTestQueue(lostLeaseQueue{queue}), service.WithHeartbeatInterval(time.Second))
		} else {
			opts = append(opts, withTestQueue(queue))
		}
		scheduler := newTestScheduler(svc, 1, time.Now, opts...)
		defer scheduler.Shutdown(context.Background())
		if err := scheduler.Schedule(context.Background(), track.UserId, track.ID, ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}
		<-queue.outcome
		got = storedAndOrphaned{store: store, queued: orphans.entries(), track: track}
	})
	return got
}

func TestRunPipeline_LeaseLossKeepsTheStoredAudioAndQueuesItForTheSweep(t *testing.T) {
	run := runJobWaitingOnItsCommit(t, true)

	if len(run.store.objects) != 1 {
		t.Fatalf("objects = %v, want the stored audio kept: the new lease owner may be serving it", run.store.objects)
	}
	want := ports.OrphanedAudio{AudioRef: refOf(run.track, ""), UserId: run.track.UserId, TrackId: run.track.ID}
	if len(run.queued) != 1 || run.queued[0] != want {
		t.Errorf("recorded = %+v, want exactly %+v", run.queued, want)
	}
}

func TestRunPipeline_ElapsedAcquireDeadlineStillDeletesTheStoredAudio(t *testing.T) {
	run := runJobWaitingOnItsCommit(t, false)

	if len(run.store.objects) != 0 {
		t.Errorf("objects = %v, want the audio deleted after the acquire deadline fired", run.store.objects)
	}
	if len(run.queued) != 0 {
		t.Errorf("recorded = %+v, want nothing queued", run.queued)
	}
}

func TestCoreSteps_StopAfterStore(t *testing.T) {
	workspace, store := scratchfiles.New(), &objectStore{objects: map[string]string{}}
	steps := coreStepsOver(service.NewSourceRegistry(tierSource{name: "only", url: "https://example.com/a"}), store, workspace)
	ac := &service.AcquisitionContext{Track: service.TrackRef{
		ID: "t1", UserID: "u1", Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours", CatalogDuration: 200,
	}}

	if err := service.RunPipeline(context.Background(), steps, ac); err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	service.CleanupTemp(context.Background(), workspace, ac)

	if ac.AudioRef == "" || !store.holds(ac.AudioRef) {
		t.Errorf("AudioRef = %q, held = %v, want the store stage to have stored the audio", ac.AudioRef, store.objects)
	}
}

const secretCookiePath = "/run/secrets/altune/yt_cookies.txt"

func ytdlpCookieErr() error {
	stderr := "ERROR: '" + secretCookiePath + "' does not look like a Netscape format cookies file " +
		"(called with --cookies " + secretCookiePath + ")"
	return fmt.Errorf("yt-dlp download: %w (stderr: %s)", errors.New("exit status 1"), stderr)
}

func captureLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("debug")
}

func recordsNamed(ring *logging.RingBuffer, message string) []logging.CapturedRecord {
	var found []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == message {
			found = append(found, r)
		}
	}
	return found
}

func assertNoCookiePath(t *testing.T, records []logging.CapturedRecord, message string) {
	t.Helper()
	if len(records) == 0 {
		t.Fatalf("no %q record logged", message)
	}
	for _, r := range records {
		text := fmt.Sprint(r.Attrs)
		if strings.Contains(text, secretCookiePath) || strings.Contains(text, "/run/secrets") {
			t.Fatalf("cookie file path leaked into %q: %v", message, r.Attrs)
		}
		if !strings.Contains(text, "exit status 1") {
			t.Fatalf("redaction dropped the diagnostic error text from %q: %v", message, r.Attrs)
		}
	}
}

type failingCommitLedger struct {
	*acquisitiontest.Ledger
	err error
}

func (l failingCommitLedger) Complete(context.Context, ids.TrackId, ids.UserId, ports.ClaimNumber, ports.Completion) error {
	return l.err
}

type panickingCommitLedger struct{ *acquisitiontest.Ledger }

func (panickingCommitLedger) Complete(context.Context, ids.TrackId, ids.UserId, ports.ClaimNumber, ports.Completion) error {
	panic("commit exploded")
}

func TestRunPipeline_APanicInTheLastStageRollsBackTheStoredAudio(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	store := newRecordingStore()

	err := storing{t, panickingCommitLedger{acquisitiontest.NewLedger(track)}, store, track}.execute()

	var stepErr *service.StepError
	if !errors.As(err, &stepErr) || stepErr.Step != "update_track" {
		t.Fatalf("Execute = %v, want a StepError naming update_track", err)
	}
	if len(store.writes) != 1 || len(store.objects) != 0 {
		t.Errorf("writes = %v, objects = %v, want the audio stored once and rolled back", store.writes, store.objects)
	}
}

type cancellingTagger struct{ cancel context.CancelFunc }

func (t cancellingTagger) Tag(context.Context, string, ports.TrackTags) error {
	t.cancel()
	return nil
}

func TestRunPipeline_ACancelledJobStopsBeforeTheNextStage(t *testing.T) {
	track := pending("Song", "Artist", "Album")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newRecordingStore()
	svc := service.MustNewAcquireTrackAudioService(acquisitiontest.NewLedger(track), sourceFor(track), store,
		scratchfiles.New(), noVerification(), noStages(taggedBy(cancellingTagger{cancel})))

	err := svc.Execute(ctx, track.UserId, track.ID)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute = %v, want the cancellation reported", err)
	}
	if len(store.writes) != 0 {
		t.Errorf("writes = %v, want the store stage never started", store.writes)
	}
}

func TestRunPipeline_StepFailedLogRedactsCookiePath(t *testing.T) {
	ring := captureLogs(t)
	track := pending("Song", "Artist", "Album")
	ledger := failingCommitLedger{Ledger: acquisitiontest.NewLedger(track), err: ytdlpCookieErr()}

	err := storing{t, ledger, newRecordingStore(), track}.execute()

	if err == nil {
		t.Fatal("expected pipeline error")
	}
	assertNoCookiePath(t, recordsNamed(ring, "pipeline step failed"), "pipeline step failed")
}

func TestRunPipeline_RollbackFailedLogRedactsCookiePath(t *testing.T) {
	ring := captureLogs(t)
	synctest.Test(t, func(t *testing.T) {
		track := pending("Song", "Artist", "Album")
		ledger := failingCommitLedger{Ledger: acquisitiontest.NewLedger(track), err: errors.New("boom")}
		store := newRecordingStore()
		store.deleteErr = ytdlpCookieErr()

		_ = storing{t, ledger, store, track}.execute()

		assertNoCookiePath(t, recordsNamed(ring, "rollback failed"), "rollback failed")
	})
}

type finesseSource struct {
	candidates []ports.AudioCandidate
	broken     string
}

func (finesseSource) Name() acqdomain.SourceName { return "finesse" }

func (s finesseSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.candidates, nil
}

func (s finesseSource) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if c.URL == s.broken {
		return ports.FetchedAudio{}, errors.New("every candidate failed")
	}
	path := filepath.Join(outDir, "track.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("audio-bytes"), 0o600)
}

const (
	finesseAlbumURL  = "https://music.youtube.com/watch?v=aaaaaaaaaaa"
	finesseUploadURL = "https://music.youtube.com/watch?v=bbbbbbbbbbb"
)

func finesseOn(url, album string) ports.AudioCandidate {
	return ports.AudioCandidate{
		Title: "Finesse", URL: url, Duration: 191, Artists: []string{"Bruno Mars"}, Album: album, Kind: ports.KindArtTrack,
	}
}

func shadowedRun(t *testing.T, source finesseSource) (*service.AcquisitionContext, []logging.CapturedRecord, error) {
	t.Helper()
	ring := captureLogs(t)
	workspace := scratchfiles.New()
	steps := coreStepsOver(service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}}, workspace)
	ac := &service.AcquisitionContext{Track: service.TrackRef{
		ID: "track-1", UserID: "u1", Title: "Finesse", Artist: "Bruno Mars", Album: "24K Magic", CatalogDuration: 191,
	}}

	err := service.RunPipeline(context.Background(), steps, ac)

	service.CleanupTemp(context.Background(), workspace, ac)
	return ac, recordsNamed(ring, "acquisition.judge_shadow"), err
}

func TestRunPipeline_ShadowJudgesTheKeptCandidateOnce(t *testing.T) {
	_, lines, err := shadowedRun(t, finesseSource{candidates: []ports.AudioCandidate{finesseOn(finesseAlbumURL, "24K Magic")}})
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("shadow lines = %d, want 1", len(lines))
	}
	attrs := lines[0].Attrs
	if attrs["pipeline_url"] != finesseAlbumURL || attrs["judge_url"] != finesseAlbumURL || attrs["verdict"] != "agree" {
		t.Fatalf("shadow line = %v, want the kept candidate judged and agreed", attrs)
	}
}

func TestRunPipeline_ShadowLogsDisagreementWithoutChangingThePick(t *testing.T) {
	source := finesseSource{
		candidates: []ports.AudioCandidate{finesseOn(finesseAlbumURL, "24K Magic"), {
			Title: "Bruno Mars - Finesse (Official Audio)", URL: finesseUploadURL, Channel: "Bruno Mars - Topic",
			Duration: 192, Categories: []string{"Music"},
		}},
		broken: finesseAlbumURL,
	}

	ac, lines, err := shadowedRun(t, source)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if ac.Selected == nil || ac.Selected.URL != finesseUploadURL {
		t.Fatalf("Selected = %+v, want the candidate that could be fetched", ac.Selected)
	}
	if len(lines) != 1 {
		t.Fatalf("shadow lines = %d, want 1", len(lines))
	}
	attrs := lines[0].Attrs
	if attrs["verdict"] != "disagree" || attrs["judge_url"] != finesseAlbumURL || attrs["pipeline_url"] != finesseUploadURL {
		t.Fatalf("shadow line = %v, want the judge on the album track and the pipeline on the upload", attrs)
	}
}

func TestRunPipeline_ShadowLogsAFailedPipeline(t *testing.T) {
	source := finesseSource{candidates: []ports.AudioCandidate{finesseOn(finesseAlbumURL, "24K Magic")}, broken: finesseAlbumURL}

	_, lines, err := shadowedRun(t, source)

	if err == nil {
		t.Fatal("RunPipeline: want the download error")
	}
	if len(lines) != 1 {
		t.Fatalf("shadow lines = %d, want 1", len(lines))
	}
	if attrs := lines[0].Attrs; attrs["verdict"] != "pipeline_failed" || attrs["pipeline_url"] != "" {
		t.Fatalf("shadow line = %v, want pipeline_failed with no kept url", attrs)
	}
}

func TestSearch_AReplaceExcludesAVideoUnderItsOtherURLSpelling(t *testing.T) {
	excluded := sourcekey.SourceKey("https://music.youtube.com/watch?v=dQw4w9WgXcQ")
	tests := []struct {
		name      string
		url       string
		wantFound bool
	}{
		{"the same video under another spelling", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", false},
		{"a different video", "https://www.youtube.com/watch?v=aaaaaaaaaaa", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{{URL: tt.url}}}
			ac := &service.AcquisitionContext{Replace: service.ReplaceState{ExcludeKeys: []string{excluded}}}

			_ = runPipeline(context.Background(), service.NewSourceRegistry(source), ac)

			if got := len(ac.Candidates) == 1; got != tt.wantFound {
				t.Errorf("candidates = %v, want kept = %v", ac.Candidates, tt.wantFound)
			}
		})
	}
}

type panicOnShadowHandler struct {
	slog.Handler
}

func (h panicOnShadowHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "acquisition.judge_shadow" {
		panic("shadow log blew up")
	}
	if err := h.Handler.Handle(ctx, r); err != nil {
		return fmt.Errorf("handle the log record: %w", err)
	}
	return nil
}

func TestRunPipeline_APanicInsideTheShadowJudgeIsLoggedAndTheRunStillSucceeds(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(panicOnShadowHandler{slog.NewTextHandler(&logs, nil)}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	workspace := scratchfiles.New()
	source := finesseSource{candidates: []ports.AudioCandidate{finesseOn(finesseAlbumURL, "24K Magic")}}
	steps := coreStepsOver(service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}}, workspace)
	ac := &service.AcquisitionContext{Track: service.TrackRef{
		ID: "track-1", UserID: "u1", Title: "Finesse", Artist: "Bruno Mars", Album: "24K Magic", CatalogDuration: 191,
	}}

	err := service.RunPipeline(context.Background(), steps, ac)

	service.CleanupTemp(context.Background(), workspace, ac)
	if err != nil {
		t.Fatalf("RunPipeline: %v, want the shadow judge's panic contained", err)
	}
	for _, want := range []string{"acquisition.judge_shadow_panic", "track_id=track-1", `panic="shadow log blew up"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want %s", logs.String(), want)
		}
	}
}

func withSpareCapacity[T any](values ...T) []T {
	out := make([]T, len(values), len(values)+4)
	copy(out, values)
	return out
}

func TestAcquisitionContext_ASnapshotSharesNoSliceOrPointerWithTheOriginal(t *testing.T) {
	delta := 1.5
	original := &service.AcquisitionContext{
		Track:           service.TrackRef{CatalogSources: withSpareCapacity(ports.CatalogSource{ExternalID: "catalog"})},
		Identity:        ports.RecordingIdentity{MBIDs: withSpareCapacity("mb-1", "mb-2"), Sources: withSpareCapacity(ports.ProviderRef{ExternalID: "ref"})},
		Candidates:      withSpareCapacity(ports.AudioCandidate{URL: "https://example.test/a"}),
		Ranked:          withSpareCapacity(ports.AudioCandidate{URL: "https://example.test/b"}),
		Selected:        &ports.AudioCandidate{URL: "https://example.test/c"},
		Attempted:       withSpareCapacity(service.AttemptedCandidate{URL: "https://example.test/d", Window: 1}),
		Evidence:        service.Evidence{Qualifiers: withSpareCapacity("live"), DurationDeltaSeconds: &delta},
		Rejections:      withSpareCapacity(rejectedcandidates.Entry{URL: "https://example.test/e"}),
		Replace:         service.ReplaceState{ExcludeKeys: withSpareCapacity("key-1")},
		PriorRejections: withSpareCapacity(ports.PriorRejection{SourceKey: "prior"}),
		Relabel:         &ports.TrackLabel{Title: "relabel"},
	}

	pass := original.Snapshot()
	pass.Track.CatalogSources[0].ExternalID = "changed"
	pass.Track.CatalogSources = append(pass.Track.CatalogSources, ports.CatalogSource{})
	pass.Identity.MBIDs[0] = "changed"
	pass.Identity.MBIDs = append(pass.Identity.MBIDs, "extra")
	pass.Identity.Sources[0].ExternalID = "changed"
	pass.Identity.Sources = append(pass.Identity.Sources, ports.ProviderRef{})
	pass.Candidates[0].URL = "changed"
	pass.Candidates = append(pass.Candidates, ports.AudioCandidate{URL: "extra"})
	pass.Ranked[0].URL = "changed"
	pass.Selected.URL = "changed"
	pass.Attempted[0].Window = 99
	pass.Attempted = append(pass.Attempted, service.AttemptedCandidate{URL: "extra"})
	pass.Evidence.Qualifiers[0] = "changed"
	pass.Evidence.Qualifiers = append(pass.Evidence.Qualifiers, "extra")
	*pass.Evidence.DurationDeltaSeconds = 99
	pass.Rejections[0].URL = "changed"
	pass.Rejections = append(pass.Rejections, rejectedcandidates.Entry{URL: "extra"})
	pass.Replace.ExcludeKeys[0] = "changed"
	pass.Replace.ExcludeKeys = append(pass.Replace.ExcludeKeys, "extra")
	pass.PriorRejections[0].SourceKey = "changed"
	pass.PriorRejections = append(pass.PriorRejections, ports.PriorRejection{})
	pass.Relabel.Title = "changed"

	checks := map[string]bool{
		"Track.CatalogSources":          len(original.Track.CatalogSources) == 1 && original.Track.CatalogSources[0].ExternalID == "catalog",
		"Identity.MBIDs":                slices.Equal(original.Identity.MBIDs, []string{"mb-1", "mb-2"}),
		"Identity.Sources":              len(original.Identity.Sources) == 1 && original.Identity.Sources[0].ExternalID == "ref",
		"Candidates":                    len(original.Candidates) == 1 && original.Candidates[0].URL == "https://example.test/a",
		"Ranked":                        original.Ranked[0].URL == "https://example.test/b",
		"Selected":                      original.Selected.URL == "https://example.test/c",
		"Attempted":                     len(original.Attempted) == 1 && original.Attempted[0].Window == 1,
		"Evidence.Qualifiers":           slices.Equal(original.Evidence.Qualifiers, []string{"live"}),
		"Evidence.DurationDeltaSeconds": *original.Evidence.DurationDeltaSeconds == 1.5,
		"Rejections":                    len(original.Rejections) == 1 && original.Rejections[0].URL == "https://example.test/e",
		"Replace.ExcludeKeys":           slices.Equal(original.Replace.ExcludeKeys, []string{"key-1"}),
		"PriorRejections":               len(original.PriorRejections) == 1 && original.PriorRejections[0].SourceKey == "prior",
		"Relabel":                       original.Relabel.Title == "relabel",
	}
	for field, untouched := range checks {
		if !untouched {
			t.Errorf("%s was changed through the snapshot, want the original untouched", field)
		}
	}
}
