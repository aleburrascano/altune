package service_test

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/tempfs"
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/rejectionlog"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/sourcekey"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type offeringSource struct {
	found    []ports.AudioCandidate
	fetchErr error

	mu      sync.Mutex
	fetched []string
}

func (s *offeringSource) Name() acqdomain.SourceName { return "offering" }

func (s *offeringSource) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.found, nil
}

func (s *offeringSource) Fetch(_ context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	s.mu.Lock()
	s.fetched = append(s.fetched, candidate.URL)
	s.mu.Unlock()
	if s.fetchErr != nil {
		return ports.FetchedAudio{}, s.fetchErr
	}
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("audio bytes"), 0o600)
}

func (s *offeringSource) fetches() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.fetched)
}

func (s *offeringSource) tried(url string) bool {
	return slices.Contains(s.fetches(), url)
}

func blindingLightsAt(urls ...string) []ports.AudioCandidate {
	out := make([]ports.AudioCandidate, 0, len(urls))
	for _, url := range urls {
		out = append(out, ports.AudioCandidate{
			Title:      "Blinding Lights",
			URL:        url,
			Channel:    "The Weeknd - Topic",
			Categories: []string{"Music"},
		})
	}
	return out
}

func readyTrackRejecting(t *testing.T, sourceURL string, rejected ...string) servedTrack {
	t.Helper()
	served := seedReadyTrack(t)
	track, _ := served.ledger.Track(served.trackID)
	track.AudioSourceURL = &sourceURL
	track.RejectedSourceKeys = rejected
	served.ledger.Seed(track)
	return served
}

func pendingSongBy(artist, title string) acquisitiontest.LedgerTrack {
	song := pendingSong()
	song.Artist, song.Title = artist, title
	return song
}

type rememberedRejections struct {
	recorded []ports.CandidateRejectionRecord
}

func (r *rememberedRejections) Record(_ context.Context, recs []ports.CandidateRejectionRecord) error {
	r.recorded = append(r.recorded, recs...)
	return nil
}

func (r *rememberedRejections) Active(context.Context, string, time.Time) ([]ports.PriorRejection, error) {
	return nil, nil
}

type lengthProber float64

func (p lengthProber) ProbeDuration(context.Context, string) (float64, error) { return float64(p), nil }

func (lengthProber) ValidateDecodable(context.Context, string) error { return nil }

func TestExecuteReplace_SeedsExclusionsFromMemoryAndTheCurrentSource(t *testing.T) {
	track := readyTrackRejecting(t, "https://music.youtube.com/watch?v=currentAAAA", "youtube:previousAAA")
	source := &offeringSource{fetchErr: errors.New("fetch disabled in this test"), found: blindingLightsAt(
		"https://www.youtube.com/watch?v=currentAAAA",
		"https://www.youtube.com/watch?v=previousAAA",
		"https://www.youtube.com/watch?v=freshAAAAAA",
	)}
	svc := service.NewAcquireTrackAudioService(track.ledger, service.NewSourceRegistry(source), track.store,
		service.WithAcquireTempWorkspace(tempfs.New()))

	_ = svc.ExecuteReplace(context.Background(), track.userID, track.trackID)

	if !source.tried("https://www.youtube.com/watch?v=freshAAAAAA") {
		t.Fatalf("the replace never reached an unexcluded candidate; tried %v", source.fetches())
	}
	if source.tried("https://www.youtube.com/watch?v=currentAAAA") {
		t.Error("the currently stored source must be excluded under any URL spelling")
	}
	if source.tried("https://www.youtube.com/watch?v=previousAAA") {
		t.Error("a source rejected by an earlier replace must stay excluded — otherwise re-acquire toggles")
	}
}

func TestExecuteReplace_RemembersTheReplacedSourceBesideTheEarlierRejectedOnes(t *testing.T) {
	track := readyTrackRejecting(t, "https://www.youtube.com/watch?v=currentAAAA", "youtube:previousAAA")

	if err := track.service().ExecuteReplace(context.Background(), track.userID, track.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	updated, _ := track.ledger.Track(track.trackID)
	if want := []string{"youtube:previousAAA", "youtube:currentAAAA"}; !slices.Equal(updated.RejectedSourceKeys, want) {
		t.Fatalf("rejected keys = %v, want %v: the earlier rejection kept and the replaced source added", updated.RejectedSourceKeys, want)
	}
	if updated.AudioSourceURL == nil || *updated.AudioSourceURL != "https://www.youtube.com/watch?v=freshAAAAAA" {
		t.Errorf("audio source = %v, want the newly stored candidate", updated.AudioSourceURL)
	}
}

func cuttingCrewRunOffered(t *testing.T, candidate ports.AudioCandidate) *service.AcquisitionContext {
	t.Helper()
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "I've Been in Love Before", Artist: "Cutting Crew", CatalogDuration: 253}}
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{candidate}}
	_ = runPipeline(context.Background(), service.NewSourceRegistry(source), ac)
	return ac
}

func TestSelect_RescuesLowIdentityOnTheArtistChannel(t *testing.T) {
	master := "https://www.youtube.com/watch?v=masterAAAAA"

	ac := cuttingCrewRunOffered(t, ports.AudioCandidate{
		Title:   "I've Been In Love Before HQ Stereo Remaster Full Length Version 1986 Special",
		URL:     master,
		Channel: "Cutting Crew - Topic",
	})

	if len(ac.Ranked) != 1 || ac.Ranked[0].URL != master {
		t.Fatalf("artist-channel candidate was dropped by the identity gate: ranked=%v", ac.Ranked)
	}
	for _, r := range ac.Rejections {
		if r.Stage != rejectionlog.Download {
			t.Errorf("a rescued candidate must not be recorded as rejected by ranking, only by the refused fetch after it: %v", ac.Rejections)
		}
	}
}

func TestSelect_DropsAndRecordsForeignLowIdentity(t *testing.T) {
	cooking := "https://www.youtube.com/watch?v=cookingAAAA"

	ac := cuttingCrewRunOffered(t, ports.AudioCandidate{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     cooking,
		Channel: "CookingChannel",
	})

	if len(ac.Ranked) != 0 {
		t.Fatalf("an unrelated candidate must stay gated: ranked=%v", ac.Ranked)
	}
	if len(ac.Rejections) != 1 || ac.Rejections[0].Stage != rejectionlog.Identity || ac.Rejections[0].URL != cooking {
		t.Fatalf("expected one identity rejection for %s, got %v", cooking, ac.Rejections)
	}
	if !strings.Contains(ac.Rejections[0].Reason, "below threshold") {
		t.Errorf("rejection reason should explain the identity gate: %q", ac.Rejections[0].Reason)
	}
}

func TestExecute_PersistsRejectionSummaryInFailureReason(t *testing.T) {
	song := pendingSongBy("Cutting Crew", "I've Been in Love Before")
	ledger := acquisitiontest.NewLedger(song)
	source := &answeringSource{name: "ytdlp", found: []ports.AudioCandidate{{
		Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
		URL:     "https://www.youtube.com/watch?v=cookingAAAA",
		Channel: "CookingChannel",
	}}}

	_ = acquireService(ledger, service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}}).
		Execute(context.Background(), song.UserId, song.ID)

	if code := persistedCode(t, ledger, song.ID); code != shared.FailureNoMatchFound {
		t.Fatalf("failure code = %q, want %q", code, shared.FailureNoMatchFound)
	}
	if got, _ := ledger.Track(song.ID); !strings.Contains(got.FailureReason, "1 identity") {
		t.Errorf("failure reason should carry the per-candidate breakdown, got %q", got.FailureReason)
	}
}

func TestExecute_AttemptCapReportsUntriedCandidatesApart(t *testing.T) {
	const attemptCap, untried = 8, 3
	song := pendingSongBy("Cutting Crew", "I've Been in Love Before")
	ledger := acquisitiontest.NewLedger(song)
	source := &offeringSource{fetchErr: errors.New("boom")}
	for i := range attemptCap + untried {
		source.found = append(source.found, ports.AudioCandidate{
			Title:   "Cutting Crew - I've Been in Love Before",
			URL:     fmt.Sprintf("https://www.youtube.com/watch?v=cuttingcr%02d", i),
			Channel: "Cutting Crew",
		})
	}

	_ = acquireService(ledger, service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}}).
		Execute(context.Background(), song.UserId, song.ID)

	if got := len(source.fetches()); got != attemptCap {
		t.Fatalf("download attempts = %d, want the cap %d", got, attemptCap)
	}
	want := fmt.Sprintf("all %d candidates rejected (%d download), %d not tried", attemptCap, attemptCap, untried)
	if got, _ := ledger.Track(song.ID); !strings.Contains(got.FailureReason, want) {
		t.Errorf("failure reason = %q, want it to contain %q", got.FailureReason, want)
	}
}

func TestExecute_AnUnmeasuredLengthSkipCountsInTheSummaryButOnlyTheProbedRejectionIsRemembered(t *testing.T) {
	const mbid = "11111111-2222-3333-4444-555555555555"
	song := pendingSongBy("The Weeknd", "Blinding Lights")
	catalogLength := 200.0
	song.CatalogDurationSeconds = &catalogLength
	ledger := acquisitiontest.NewLedger(song)
	offered := blindingLightsAt("https://www.youtube.com/watch?v=shortshort1", "https://www.youtube.com/watch?v=probedAAAAA")
	offered[0].Duration = 30
	short, probed := offered[0].URL, offered[1].URL
	source := &offeringSource{found: offered}
	remembered := &rememberedRejections{}
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}},
		service.WithAcquireTempWorkspace(tempfs.New()), service.WithRejectionStore(remembered),
		service.WithAudioProber(lengthProber(30)), service.WithRecordingResolver(resolvedTo(mbid)))

	_ = svc.Execute(context.Background(), song.UserId, song.ID)

	if source.tried(short) {
		t.Fatalf("download attempts = %v, the search-length candidate must never be downloaded", source.fetches())
	}
	if got, _ := ledger.Track(song.ID); !strings.Contains(got.FailureReason, "all 2 candidates rejected (2 duration)") {
		t.Errorf("failure reason = %q, want both the unmeasured skip and the probed rejection counted", got.FailureReason)
	}
	if len(remembered.recorded) != 1 {
		t.Fatalf("remembered = %+v, want only the probed rejection of %q", remembered.recorded, probed)
	}
	got := remembered.recorded[0]
	basis := ports.RejectionBasis{ExpectedSeconds: catalogLength, ReferenceMBID: mbid}
	if got.TrackID != song.ID.String() || got.SourceKey != sourcekey.SourceKey(probed) || got.Reason != string(rejectionlog.Duration) || got.Basis != basis {
		t.Errorf("remembered %+v, want track %s, source %q, reason %q and basis %+v",
			got, song.ID, sourcekey.SourceKey(probed), rejectionlog.Duration, basis)
	}
}
