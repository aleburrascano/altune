package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type progressStages struct {
	mu     sync.Mutex
	stages []string
}

func (p *progressStages) Publish(_ context.Context, _ ids.UserId, payload events.Payload) {
	progress, ok := payload.(events.TrackAcquisitionProgress)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stages = append(p.stages, progress.Stage)
}

func (p *progressStages) reported() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.stages)
}

type finishedJob struct {
	record ports.JobRecord
	stages []string
}

func scheduleAndFinish(t *testing.T, source ports.AudioSource, opts ...func(*service.AcquireTrackAudioService)) finishedJob {
	t.Helper()
	return scheduleAndFinishWith(t, source, noVerification(), opts...)
}

func scheduleAndFinishWith(t *testing.T, source ports.AudioSource, wiring service.VerificationPorts, opts ...func(*service.AcquireTrackAudioService)) finishedJob {
	t.Helper()
	var job finishedJob
	synctest.Test(t, func(t *testing.T) {
		track := pendingBlindingLights()
		length := blindingLightsLength
		track.CatalogDurationSeconds = &length
		ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
		svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}}, scratchfiles.New(), wiring, noStages(), opts...)
		progress := &progressStages{}
		scheduler := service.NewBackgroundAcquisitionScheduler(svc, 1, service.WithSchedulerEvents(progress))
		defer scheduler.Shutdown(context.Background())
		if err := scheduler.Schedule(context.Background(), track.UserId, track.ID, ""); err != nil {
			t.Fatalf("Schedule = %v, want nil", err)
		}

		time.Sleep(10 * time.Second)
		synctest.Wait()

		recent := scheduler.Status().Recent
		if len(recent) != 1 || recent[0].TrackID != track.ID.String() {
			t.Fatalf("Recent = %+v, want the one finished job of %s", recent, track.ID)
		}
		job = finishedJob{record: recent[0], stages: progress.reported()}
	})
	if t.Failed() {
		t.FailNow()
	}
	return job
}

type timedSource struct {
	perURLSource
	takes map[string]time.Duration
}

func (s timedSource) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	time.Sleep(s.takes[candidate.URL])
	return s.perURLSource.Fetch(ctx, candidate, outDir)
}

var pipelineStages = []string{"search", "select", "download", "tag", "store", "update_track"}

const (
	failingRank1URL = "https://music.youtube.com/watch?v=rank1AAAAAA"
	storedRank2URL  = "https://www.youtube.com/watch?v=rank2AAAAAA"
)

func TestStatus_AJobReportsEveryPipelineStageInOrderAndTheSourceItStored(t *testing.T) {
	job := scheduleAndFinish(t, freshAudioSource{})

	if !slices.Equal(job.stages, pipelineStages) {
		t.Errorf("progress stages = %v, want %v", job.stages, pipelineStages)
	}
	if want := "https://www.youtube.com/watch?v=freshAAAAAA"; job.record.ResolvedSource != want {
		t.Errorf("resolved source = %q, want %q, the selected candidate", job.record.ResolvedSource, want)
	}
}

func TestStatus_AJobTimesEveryPipelineStageThatRanAndOnlyThose(t *testing.T) {
	source := perURLSource{found: blindingLightsAt(failingRank1URL), broken: map[string]bool{failingRank1URL: true}}

	job := scheduleAndFinish(t, source)

	got := []string{}
	for stage := range job.record.StageMs {
		if slices.Contains(pipelineStages, stage) {
			got = append(got, stage)
		}
	}
	slices.Sort(got)
	if want := []string{"download", "search", "select"}; !slices.Equal(got, want) {
		t.Errorf("timed pipeline stages = %v, want %v: the failed stage is timed, the skipped ones are absent", got, want)
	}
}

func TestStatus_AJobSumsTheTimeOfEveryFetchAndReportsTheFormatAndFileItStored(t *testing.T) {
	format := ports.SourceFormat{ID: "140", Codec: "mp4a.40.2"}
	source := timedSource{
		perURLSource: perURLSource{
			found:   blindingLightsAt(failingRank1URL, storedRank2URL),
			formats: map[string]ports.SourceFormat{storedRank2URL: format},
			broken:  map[string]bool{failingRank1URL: true},
		},
		takes: map[string]time.Duration{failingRank1URL: 120 * time.Millisecond, storedRank2URL: 30 * time.Millisecond},
	}

	job := scheduleAndFinish(t, source)

	if got := job.record.StageMs["download.full"]; got != 150 {
		t.Errorf("download.full = %dms, want 150, summed across both candidates' fetches", got)
	}
	if job.record.Source != format {
		t.Errorf("source format = %+v, want %+v, the format of the file it kept", job.record.Source, format)
	}
	if want := (ports.StoredFile{Bytes: int64(len("audio bytes")), Codec: "mp3"}); job.record.Stored != want {
		t.Errorf("stored file = %+v, want %+v", job.record.Stored, want)
	}
}

func TestStatus_AJobReportsTheStoredCandidateAsItsSource(t *testing.T) {
	source := perURLSource{found: blindingLightsAt(failingRank1URL, storedRank2URL), broken: map[string]bool{failingRank1URL: true}}

	job := scheduleAndFinishWith(t, source, resolving(resolvedTo(blindingLightsRecording), verifiedBy(&identifier{})))

	if job.record.State != ports.JobSucceeded || job.record.ResolvedSource != storedRank2URL {
		t.Errorf("job = %v from %q, want succeeded from the stored rank-2 candidate %q", job.record.State, job.record.ResolvedSource, storedRank2URL)
	}
}
