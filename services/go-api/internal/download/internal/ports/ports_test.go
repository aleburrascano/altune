package ports_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func runningContext(t *testing.T) context.Context { return t.Context() }

func cancelledContext(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func expiredContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	t.Cleanup(cancel)
	return ctx
}

func TestJob_AJobWithoutAReporterReportsToNobody(t *testing.T) {
	var job ports.AcquisitionJob

	job.Progress().Meta("title", "artist", "album")
	job.Progress().Stage(context.Background(), "search")
}

func TestJob_VerifyCommitAsksTheCheckItCarries(t *testing.T) {
	refused := errors.New("lease lost")
	cases := []struct {
		name string
		job  ports.AcquisitionJob
		want error
	}{
		{"no check admits the commit", ports.AcquisitionJob{}, nil},
		{"a refusing check refuses it", ports.AcquisitionJob{CommitCheck: func(context.Context) error { return refused }}, refused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.job.VerifyCommit(context.Background()); !errors.Is(got, tc.want) {
				t.Errorf("VerifyCommit = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJob_OnlyASchedulerOwnedJobTreatsItsContextEndingAsTheSchedulersDoing(t *testing.T) {
	cases := []struct {
		name          string
		job           ports.AcquisitionJob
		ctx           func(*testing.T) context.Context
		wantCancelled bool
		wantHandedOff bool
	}{
		{"owned and cancelled", ports.AcquisitionJob{SchedulerOwned: true}, cancelledContext, true, true},
		{"owned and past its deadline", ports.AcquisitionJob{SchedulerOwned: true}, expiredContext, true, false},
		{"owned and still running", ports.AcquisitionJob{SchedulerOwned: true}, runningContext, false, false},
		{"not owned and cancelled", ports.AcquisitionJob{}, cancelledContext, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx(t)
			if got := tc.job.CancelledByScheduler(ctx); got != tc.wantCancelled {
				t.Errorf("CancelledByScheduler = %v, want %v", got, tc.wantCancelled)
			}
			if got := tc.job.HandedOverByScheduler(ctx); got != tc.wantHandedOff {
				t.Errorf("HandedOverByScheduler = %v, want %v", got, tc.wantHandedOff)
			}
		})
	}
}

type stubSource struct {
	name       domain.SourceName
	candidates []ports.AudioCandidate
	err        error
}

func (s stubSource) Name() domain.SourceName { return s.name }

func (s stubSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.candidates, s.err
}

func (s stubSource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("stubSource does not fetch")
}

func unavailable() error {
	return &domain.SourceUnavailableError{Source: "yt", Err: errors.New("429")}
}

func isSourceUnavailable(err error) bool {
	var u *domain.SourceUnavailableError
	return errors.As(err, &u)
}

func findBesideFailure(failure error) ([]ports.AudioCandidate, error) {
	return service.NewSourceRegistry(stubSource{name: "a", err: failure}, stubSource{name: "b"}).
		Find(context.Background(), ports.FindRequest{Title: "t", Artist: "a"})
}

func TestFind_EmptyMergeBesideUnavailableFailureIsUnavailable(t *testing.T) {
	got, err := findBesideFailure(unavailable())

	if !isSourceUnavailable(err) || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty and a source-unavailable error", got, err)
	}
}

func TestFind_EmptyMergeBesidePlainFailureIsDegraded(t *testing.T) {
	parseErr := errors.New("parse")

	_, err := findBesideFailure(parseErr)

	if !errors.Is(err, domain.ErrSearchDegraded) || !errors.Is(err, parseErr) {
		t.Fatalf("err = %v, want ErrSearchDegraded wrapping the parse failure", err)
	}
}

func TestFindReportingOutage_KeepsCandidatesBesideAnUnavailableFailure(t *testing.T) {
	want := downloadtest.Candidate("https://example.com/a")
	registry := service.NewSourceRegistry(
		stubSource{name: "a", err: unavailable()},
		stubSource{name: "b", candidates: []ports.AudioCandidate{want}})

	got, err := registry.FindReportingOutage(context.Background(), ports.FindRequest{Title: "t", Artist: "a"})

	if err != nil || len(got.Candidates) != 1 || got.Candidates[0].URL != want.URL || !isSourceUnavailable(got.Outage) {
		t.Fatalf("got %+v, err %v; want the candidate and the outage", got, err)
	}
}

func TestFindReportingOutage_MergesEverySource(t *testing.T) {
	sources := make([]ports.AudioSource, 0, 3)
	for i := range 3 {
		out := make([]ports.AudioCandidate, 0, 5)
		for j := range 5 {
			out = append(out, downloadtest.Candidate(fmt.Sprintf("https://example.com/%d/%d", i, j)))
		}
		sources = append(sources, stubSource{name: domain.SourceName(fmt.Sprint(i)), candidates: out})
	}

	got, err := service.NewSourceRegistry(sources...).FindReportingOutage(context.Background(), ports.FindRequest{Title: "t", Artist: "a"})

	if err != nil || got.Outage != nil || len(got.Candidates) != 15 {
		t.Fatalf("got %d candidates, outage %v, err %v; want all 15 from 3 sources", len(got.Candidates), got.Outage, err)
	}
}

func TestSourceFormat_IsLowBitrate(t *testing.T) {
	cases := []struct {
		name   string
		format ports.SourceFormat
		floor  int
		want   bool
	}{
		{"the muxed fallback is low", ports.SourceFormat{ID: "18", Kbps: 96}, 112, true},
		{"the muxed fallback is low whatever its reported bitrate", ports.SourceFormat{ID: "18"}, 112, true},
		{"a bitrate under the floor is low", ports.SourceFormat{ID: "139", Kbps: 96}, 112, true},
		{"a bitrate above the floor is not low", ports.SourceFormat{ID: "140", Kbps: 130}, 112, false},
		{"a bitrate at the floor is not low", ports.SourceFormat{ID: "140", Kbps: 112}, 112, false},
		{"an unknown bitrate on another format is not low", ports.SourceFormat{ID: "140"}, 112, false},
		{"a zero floor never calls the muxed fallback low", ports.SourceFormat{ID: "18", Kbps: 96}, 0, false},
		{"a zero floor never calls a low bitrate low", ports.SourceFormat{ID: "139", Kbps: 48}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.format.IsLowBitrate(tc.floor); got != tc.want {
				t.Errorf("%+v.IsLowBitrate(%d) = %v, want %v", tc.format, tc.floor, got, tc.want)
			}
		})
	}
}

func TestAcquisitionVerification_ToolsPresentNeedsEveryTool(t *testing.T) {
	all := ports.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true}
	if !all.ToolsPresent() {
		t.Fatalf("%+v.ToolsPresent() = false, want true with every tool present", all)
	}
	missing := map[string]func(*ports.Verification){
		"ffprobe": func(v *ports.Verification) { v.Ffprobe = false },
		"ffmpeg":  func(v *ports.Verification) { v.Ffmpeg = false },
		"fpcalc":  func(v *ports.Verification) { v.Fpcalc = false },
		"yt-dlp":  func(v *ports.Verification) { v.YtDlp = false },
	}
	for tool, drop := range missing {
		v := all
		drop(&v)
		if v.ToolsPresent() {
			t.Errorf("without %s: ToolsPresent() = true, want false", tool)
		}
	}
}

func TestAppendRejectedSourceKeys_IsIdempotentIgnoresEmptyAndBounded(t *testing.T) {
	t.Parallel()

	keys := ports.AppendRejectedSourceKeys(nil, []string{"youtube:aaa", "youtube:aaa", ""})
	if !reflect.DeepEqual(keys, []string{"youtube:aaa"}) {
		t.Fatalf("keys = %v, want one key with the duplicate and the empty key ignored", keys)
	}

	var added []string
	for i := range 60 {
		added = append(added, string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	keys = ports.AppendRejectedSourceKeys(nil, added)
	if want := added[len(added)-ports.MaxRejectedSourceKeys:]; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want the %d most recent rejections %v", keys, ports.MaxRejectedSourceKeys, want)
	}
}

func TestMergeUpgradeEvidence_StateOwnsTheUpgradeKeysAndTheTrackOwnsTheRest(t *testing.T) {
	t.Parallel()

	track := json.RawMessage(`{"grade":"B","verdict":"match","upgrade_tries":7,"in_place_failures":2}`)
	state := json.RawMessage(`{"upgrade_tries":1}`)

	merged, err := ports.MergeUpgradeEvidence(track, state)
	if err != nil {
		t.Fatalf("MergeUpgradeEvidence = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged evidence is not JSON: %v", err)
	}
	want := map[string]any{"grade": "B", "verdict": "match", "upgrade_tries": float64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %v, want %v (a stale upgrade key on the track must not leak through)", got, want)
	}
}

func TestMergeUpgradeEvidence_NoEvidenceStaysNone(t *testing.T) {
	t.Parallel()

	merged, err := ports.MergeUpgradeEvidence(nil, nil)
	if err != nil || len(merged) != 0 {
		t.Errorf("MergeUpgradeEvidence(nil, nil) = %q, %v, want empty and nil", merged, err)
	}
}

func TestMergeUpgradeEvidence_ANullTrackEvidenceTakesTheStateKeysWithoutPanicking(t *testing.T) {
	t.Parallel()

	merged, err := ports.MergeUpgradeEvidence(json.RawMessage(`null`), json.RawMessage(`{"upgrade_tries":1}`))
	if err != nil || string(merged) != `{"upgrade_tries":1}` {
		t.Errorf("MergeUpgradeEvidence = %s, %v, want the state keys", merged, err)
	}
}
