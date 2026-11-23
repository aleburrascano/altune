package service_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/tempfs"
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/grade"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared/events"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestExecute_MarksTheTrackReadyOnTheFileItStored(t *testing.T) {
	track := pendingBlindingLights()
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
	store := &objectStore{objects: map[string]string{}}
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(freshAudioSource{}), store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	err := svc.Execute(context.Background(), track.UserId, track.ID)

	requireReady(t, ledger, track.ID, err)
	want := service.BuildAudioRef(service.TrackRef{
		ID: track.ID.String(), UserID: track.UserId.String(), Title: track.Title, Artist: track.Artist, Album: track.Album,
	}, "audio.mp3")
	if got, _ := ledger.Track(track.ID); got.AudioRef == nil || *got.AudioRef != want {
		t.Fatalf("audio ref = %v, want %q", got.AudioRef, want)
	}
	if !store.holds(want) {
		t.Errorf("the store does not hold %q, the file the track now points at", want)
	}
}

func TestExecuteReplace_SwapsTheServingFileForTheOneItStored(t *testing.T) {
	served := seedReadyTrack(t)

	if err := served.service().ExecuteReplace(context.Background(), served.userID, served.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	got, _ := served.ledger.Track(served.trackID)
	if got.Status != ports.TrackReady || got.AudioRef == nil || *got.AudioRef == served.originalRef {
		t.Fatalf("track = %v on %v, want ready on a new file, not the original %q", got.Status, got.AudioRef, served.originalRef)
	}
	if !served.store.holds(*got.AudioRef) {
		t.Errorf("the store does not hold %q, the file the track now serves", *got.AudioRef)
	}
}

type vanishingLedger struct{ *acquisitiontest.Ledger }

func (vanishingLedger) Complete(context.Context, shared.TrackId, shared.UserId, ports.Fence, ports.Completion) error {
	return ports.ErrTrackNotFound
}

func TestExecute_ATrackGoneAtTheCommitFailsTheRunAsAcquisitionFailed(t *testing.T) {
	track := pendingBlindingLights()
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
	svc := service.NewAcquireTrackAudioService(vanishingLedger{ledger}, service.NewSourceRegistry(freshAudioSource{}),
		&objectStore{objects: map[string]string{}}, service.WithAcquireTempWorkspace(tempfs.New()))

	err := svc.Execute(context.Background(), track.UserId, track.ID)

	if err == nil || !strings.Contains(err.Error(), "track not found for update") {
		t.Fatalf("Execute = %v, want the update step's track-not-found failure", err)
	}
	if code := persistedCode(t, ledger, track.ID); code != shared.FailureAcquisitionFailed {
		t.Errorf("persisted failure code = %q, want %q: failure_reason is stored and shown to the client", code, shared.FailureAcquisitionFailed)
	}
}

type progressRecorder struct {
	mu     sync.Mutex
	stages []string
}

func (p *progressRecorder) Publish(_ context.Context, _ shared.UserId, eventType events.EventType, payload map[string]any) {
	if eventType != events.TypeTrackAcquisitionProgress {
		return
	}
	stage, _ := payload["stage"].(string)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stages = append(p.stages, stage)
}

func (p *progressRecorder) published() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.stages)
}

func TestScheduledRun_TellsTheClientWhenItReachesTheUpdateTrackStage(t *testing.T) {
	track := acquisitiontest.LedgerTrack{TrackSnapshot: pendingBlindingLights()}
	ledger := acquisitiontest.NewLedger(track)
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(freshAudioSource{}),
		&objectStore{objects: map[string]string{}}, service.WithAcquireTempWorkspace(tempfs.New()))
	progress := &progressRecorder{}

	outcome := scheduleOnce(t, svc, track, nil, service.WithSchedulerEvents(progress))

	if !outcome.settled {
		t.Fatalf("job outcome = %+v, want the run settled", outcome)
	}
	if stages := progress.published(); !slices.Contains(stages, "update_track") {
		t.Errorf("progress stages = %v, want %q among them: the mobile client renders this name", stages, "update_track")
	}
}

func TestReplaceGated_AnUnprobedFileGetsNoLengthRescue(t *testing.T) {
	const recording = "mb-blinding"
	served := seedReadyTrack(t)
	source := &offeringSource{found: []ports.AudioCandidate{{
		Title: "The Weeknd - Blinding Lights", URL: "https://www.youtube.com/watch?v=fanUploadAA",
		Channel: "Some Fan", Kind: ports.KindUpload, Duration: 285, Categories: []string{"Music"},
	}}}
	svc := service.NewAcquireTrackAudioService(served.ledger, service.NewSourceRegistry(source), served.store,
		service.WithAcquireTempWorkspace(tempfs.New()),
		service.WithRecordingResolver(&resolver{identity: ports.RecordingIdentity{MBID: recording, MBIDs: []string{recording}}}),
		service.WithAudioIdentifier(&identifier{match: ports.RecordingMatch{AcoustID: "ac-1", Results: []ports.AcoustIDResult{
			{Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: recording}}},
		}}}))
	snapshot, err := served.ledger.Get(context.Background(), served.trackID, served.userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gate := upgrade.Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}

	_, err = svc.ReplaceGated(context.Background(), served.userID, snapshot, gate)

	if !errors.Is(err, upgrade.ErrNotAnUpgrade) {
		t.Fatalf("ReplaceGated = %v, want ErrNotAnUpgrade: the source's claimed 285 s was never measured", err)
	}
	if !source.tried("https://www.youtube.com/watch?v=fanUploadAA") {
		t.Fatalf("the fan upload was never fetched, so the gate never judged it; tried %v", source.fetches())
	}
	if got, _ := served.ledger.Track(served.trackID); got.AudioRef == nil || *got.AudioRef != served.originalRef {
		t.Errorf("audio ref = %v, want the original %q kept", got.AudioRef, served.originalRef)
	}
}

const (
	blindingLightsRecording = "mb-blinding-lights"
	blindingLightsLength    = 200.0
)

func hardFingerprintOn(mbid string) *identifier {
	return &identifier{match: ports.RecordingMatch{AcoustID: "ac-1", Results: []ports.AcoustIDResult{
		{Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: mbid}}},
	}}}
}

type storedAcquisition struct {
	track    acquisitiontest.LedgerTrack
	evidence service.Evidence
	keys     map[string]json.RawMessage
}

func acquireStoring(t *testing.T, candidate ports.AudioCandidate, opts ...func(*service.AcquireTrackAudioService)) storedAcquisition {
	t.Helper()
	track := pendingBlindingLights()
	length := blindingLightsLength
	track.CatalogDurationSeconds = &length
	ledger, err := acquireWith(t, track, &offeringSource{found: []ports.AudioCandidate{candidate}}, opts...)
	requireReady(t, ledger, track.ID, err)
	got, _ := ledger.Track(track.ID)
	stored := storedAcquisition{track: got}
	if err := json.Unmarshal(got.Evidence, &stored.evidence); err != nil {
		t.Fatalf("evidence not stored as JSON: %v (%q)", err, got.Evidence)
	}
	if err := json.Unmarshal(got.Evidence, &stored.keys); err != nil {
		t.Fatalf("evidence not a JSON object: %v (%q)", err, got.Evidence)
	}
	return stored
}

func blindingLightsBy(channel string, kind ports.CandidateKind) ports.AudioCandidate {
	return ports.AudioCandidate{
		Title: "The Weeknd - Blinding Lights", URL: "https://www.youtube.com/watch?v=blindingAAA",
		Channel: channel, Kind: kind, Categories: []string{"Music"},
	}
}

func TestExecute_StoresTheVerdictScoreSourceTitleAndTheirConfidence(t *testing.T) {
	candidate := blindingLightsBy("The Weeknd - Topic", ports.KindUpload)
	candidate.Title = "The Weeknd - Blinding Lights (Official Audio)"

	stored := acquireStoring(t, candidate,
		service.WithRecordingResolver(resolvedTo(blindingLightsRecording)), service.WithAudioIdentifier(hardFingerprintOn(blindingLightsRecording)))

	if e := stored.evidence; e.Verdict != "hard" || e.AcoustIDScore != 0.97 || e.SourceTitle != candidate.Title {
		t.Errorf("evidence = %+v, want verdict hard, acoustid_score 0.97 and the source title %q", e, candidate.Title)
	}
	if want := service.ScoreConfidence(stored.evidence, blindingLightsLength); stored.track.Confidence != want {
		t.Errorf("stored confidence = %v, want %v, the score of the stored evidence", stored.track.Confidence, want)
	}
}

func TestExecute_StoresTheSourceKindGenerationAndGradePerKind(t *testing.T) {
	cases := []struct {
		name       string
		channel    string
		kind       ports.CandidateKind
		omv        bool
		identifier *identifier
		wantKind   grade.SourceKind
		wantGrade  grade.Grade
	}{
		{"art track proven by fingerprint", "The Weeknd - Topic", ports.KindArtTrack, false, hardFingerprintOn(blindingLightsRecording), grade.ArtTrack, grade.A},
		{"official video proven by fingerprint", "The Weeknd", ports.KindPinnedVideo, true, hardFingerprintOn(blindingLightsRecording), grade.OfficialVideo, grade.A},
		{"pinned video never seen as official, proven by fingerprint", "The Weeknd", ports.KindPinnedVideo, false, hardFingerprintOn(blindingLightsRecording), grade.FanUpload, grade.B},
		{"artist's own upload proven by fingerprint", "The Weeknd", ports.KindUpload, false, hardFingerprintOn(blindingLightsRecording), grade.LabelUpload, grade.A},
		{"fan upload proven by fingerprint", "Some Fan", ports.KindUpload, false, hardFingerprintOn(blindingLightsRecording), grade.FanUpload, grade.B},
		{"fan upload with no fingerprint answer", "Some Fan", ports.KindUpload, false, nil, grade.FanUpload, grade.D},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := blindingLightsBy(tc.channel, tc.kind)
			candidate.ObservedOMV = tc.omv
			opts := []func(*service.AcquireTrackAudioService){service.WithRecordingResolver(resolvedTo(blindingLightsRecording))}
			if tc.identifier != nil {
				opts = append(opts, service.WithAudioIdentifier(tc.identifier))
			}

			e := acquireStoring(t, candidate, opts...).evidence

			if e.SourceKind != tc.wantKind || e.Grade != tc.wantGrade || e.PipelineGeneration != grade.PipelineGeneration {
				t.Errorf("source_kind=%q grade=%q generation=%d, want %q %q %d", e.SourceKind, e.Grade, e.PipelineGeneration, tc.wantKind, tc.wantGrade, grade.PipelineGeneration)
			}
		})
	}
}

func TestExecute_ADoubtedReferenceNeverGradesA(t *testing.T) {
	identity := ports.RecordingIdentity{MBID: blindingLightsRecording, MBIDs: []string{blindingLightsRecording}, ReferenceDoubted: true}

	e := acquireStoring(t, blindingLightsBy("The Weeknd - Topic", ports.KindArtTrack),
		service.WithRecordingResolver(&resolver{identity: identity}), service.WithAudioIdentifier(hardFingerprintOn(blindingLightsRecording))).evidence

	if e.Grade != grade.B {
		t.Errorf("grade = %q, want B: a fingerprint on a doubted reference never proves the recording", e.Grade)
	}
}

func TestExecute_AnUncomparedLengthIsStoredAsNullInTheEvidence(t *testing.T) {
	stored := acquireStoring(t, blindingLightsBy("The Weeknd - Topic", ports.KindUpload))

	if v, present := stored.keys["duration_delta_seconds"]; !present || string(v) != "null" {
		t.Errorf("duration_delta_seconds = %s (present %v), want null: no prober measured the file", v, present)
	}
}

func TestExecute_StoredEvidenceHasNoUnsetISRCHit(t *testing.T) {
	stored := acquireStoring(t, blindingLightsBy("The Weeknd - Topic", ports.KindUpload))

	if _, present := stored.keys["isrc_set_hit"]; present {
		t.Errorf("stored evidence carries isrc_set_hit: %s", stored.track.Evidence)
	}
}

type perURLSource struct {
	found   []ports.AudioCandidate
	formats map[string]ports.SourceFormat
	broken  map[string]bool
}

func (perURLSource) Name() acqdomain.SourceName { return "per-url" }

func (s perURLSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.found, nil
}

func (s perURLSource) Fetch(_ context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	if s.broken[candidate.URL] {
		return ports.FetchedAudio{}, errors.New("fetch failed")
	}
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path, Format: s.formats[candidate.URL]}, os.WriteFile(path, []byte("audio bytes"), 0o600)
}

func TestExecute_StoresTheSourceURLOfTheFileItKept(t *testing.T) {
	const first, second = "https://www.youtube.com/watch?v=firstAAAAAA", "https://www.youtube.com/watch?v=secondAAAAA"
	muxed := ports.SourceFormat{ID: ports.MuxedFallbackFormatID}
	fingerprinted := service.WithAudioIdentifier(hardFingerprintOn(blindingLightsRecording))
	cases := []struct {
		name       string
		firstTitle string
		formats    map[string]ports.SourceFormat
		broken     map[string]bool
		opts       []func(*service.AcquireTrackAudioService)
		want       string
	}{
		{
			"a tagged, fingerprint-proven file beside one that failed to fetch", "", nil,
			map[string]bool{first: true},
			[]func(*service.AcquireTrackAudioService){fingerprinted, service.WithAudioTagger(&recordingTagger{})},
			second,
		},
		{"an unproven file beside one that failed to fetch", "", nil, map[string]bool{first: true}, nil, second},
		{
			"a fingerprinted other version on length, held as best effort when nothing better came", "The Weeknd - Blinding Lights (Radio Edit)", nil,
			map[string]bool{second: true},
			[]func(*service.AcquireTrackAudioService){fingerprinted, service.WithAudioProber(lengthProber(blindingLightsLength))},
			first,
		},
		{
			"a muxed file when nothing better came", "",
			map[string]ports.SourceFormat{first: muxed},
			map[string]bool{second: true},
			[]func(*service.AcquireTrackAudioService){fingerprinted},
			first,
		},
		{
			"an audio-only file chosen over a muxed one", "",
			map[string]ports.SourceFormat{first: muxed},
			nil,
			[]func(*service.AcquireTrackAudioService){fingerprinted},
			second,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := []ports.AudioCandidate{blindingLightsBy("The Weeknd - Topic", ports.KindArtTrack), blindingLightsBy("The Weeknd - Topic", ports.KindArtTrack)}
			found[0].URL, found[1].URL = first, second
			if tc.firstTitle != "" {
				found[0].Title = tc.firstTitle
			}
			opts := append([]func(*service.AcquireTrackAudioService){service.WithRecordingResolver(resolvedTo(blindingLightsRecording))}, tc.opts...)
			track := pendingBlindingLights()
			length := blindingLightsLength
			track.CatalogDurationSeconds = &length

			ledger, err := acquireWith(t, track, perURLSource{found: found, formats: tc.formats, broken: tc.broken}, opts...)

			requireReady(t, ledger, track.ID, err)
			if got, _ := ledger.Track(track.ID); got.AudioSourceURL == nil || *got.AudioSourceURL != tc.want {
				t.Errorf("stored source URL = %v, want %q, the file the run kept", got.AudioSourceURL, tc.want)
			}
		})
	}
}
