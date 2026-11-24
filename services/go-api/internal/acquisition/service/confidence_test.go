package service_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/core/isrcproof"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/recordingresolver"
	"altune/go-api/internal/acquisition/service"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"testing"
)

const (
	channelTopic  = "topic"
	channelArtist = "artist"
	channelOther  = "other"

	confidenceFloor = 0.5
)

func TestSourceKindOf(t *testing.T) {
	cases := []struct {
		name    string
		kind    acquisition.CandidateKind
		omv     bool
		channel string
		want    acquisition.SourceKind
	}{
		{"art track", acquisition.KindArtTrack, false, channelOther, acquisition.ArtTrack},
		{"observed official video", acquisition.KindPinnedVideo, true, channelOther, acquisition.OfficialVideo},
		{"pinned video of an unknown type is not official", acquisition.KindPinnedVideo, false, channelTopic, acquisition.FanUpload},
		{"upload from the artist's channel", acquisition.KindUpload, false, channelArtist, acquisition.LabelUpload},
		{"upload from a topic channel", acquisition.KindUpload, false, channelTopic, acquisition.LabelUpload},
		{"upload from any other channel", acquisition.KindUpload, false, channelOther, acquisition.FanUpload},
		{"catalog pin is not assumed official", acquisition.KindCatalogPin, false, channelTopic, acquisition.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acquisition.SourceKindOf(acquisition.AudioCandidate{Kind: tc.kind, ObservedOMV: tc.omv}, tc.channel); got != tc.want {
				t.Errorf("SourceKindOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUploaderSourceKind(t *testing.T) {
	cases := []struct {
		name, artist, channel string
		want                  acquisition.SourceKind
	}{
		{"the artist's own channel", "Daft Punk", "Daft Punk", acquisition.LabelUpload},
		{"a topic channel", "Daft Punk", "Daft Punk - Topic", acquisition.LabelUpload},
		{"someone else's channel", "Daft Punk", "lyrics hub", acquisition.FanUpload},
		{"no channel", "Daft Punk", "", acquisition.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acquisition.UploaderSourceKind(tc.artist, tc.channel); got != tc.want {
				t.Errorf("UploaderSourceKind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFingerprintOf(t *testing.T) {
	cases := map[acquisition.VerdictKind]acquisition.GradeFingerprint{
		acquisition.VerdictHard:          acquisition.Matches,
		acquisition.VerdictSoft:          acquisition.Approximates,
		acquisition.VerdictOtherVersion:  acquisition.Contradicts,
		acquisition.VerdictDifferentSong: acquisition.FingerprintUnsupported,
		acquisition.VerdictUnknown:       acquisition.FingerprintSilent,
		"":                               acquisition.FingerprintSilent,
	}
	for verdict, want := range cases {
		if got := acquisition.FingerprintOf(verdict); got != want {
			t.Errorf("FingerprintOf(%q) = %v, want %v", verdict, got, want)
		}
	}
}

func TestScoreConfidence(t *testing.T) {
	tests := []struct {
		name  string
		e     acquisition.ConfidenceEvidence
		track float64
		want  float64
	}{
		{"hard", acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.97}, 204, 0.95},
		{"soft", acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.95}, 204, 0.85},
		{"hard with a low score", acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.8}, 204, 0.85},
		{"soft with a low score stays at the unknown cap", acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.8}, 204, 0.8},
		{"hard at exactly the low-score cutoff keeps full confidence", acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.92}, 204, 0.95},
		{"hard with no score recorded", acquisition.ConfidenceEvidence{Verdict: "hard"}, 204, 0.95},
		{"topic one second off", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelTopic, DurationDeltaSeconds: secs(1)}, 204, 0.7},
		{"other channel twenty seconds off", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelOther, DurationDeltaSeconds: secs(20)}, 204, 0.2},
		{"artist channel on the spot", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelArtist, DurationDeltaSeconds: secs(0)}, 204, 0.7},
		{"no fingerprint run", acquisition.ConfidenceEvidence{Channel: channelTopic, DurationDeltaSeconds: secs(-1)}, 204, 0.7},
		{"topic twenty seconds short earns no length bonus", acquisition.ConfidenceEvidence{Channel: channelTopic, DurationDeltaSeconds: secs(-20)}, 204, 0.5},
		{"loose duration window", acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(10)}, 204, 0.3},
		{"loose window grows with the track", acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(20)}, 400, 0.3},
		{"qualifier costs 0.2", acquisition.ConfidenceEvidence{Channel: channelTopic, DurationDeltaSeconds: secs(0), Qualifiers: []string{"radio edit"}}, 204, 0.5},
		{"resolved adds 0.1 and the cap holds", acquisition.ConfidenceEvidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}, 204, 0.8},
		{"unknown length earns no length bonus", acquisition.ConfidenceEvidence{Verdict: "unknown", Channel: channelTopic}, 204, 0.5},
		{"never below zero", acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}, 204, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acquisition.ScoreConfidence(tt.e, tt.track); got != tt.want {
				t.Errorf("ScoreConfidence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreConfidence_ALengthExactlyAtEachToleranceEarnsThatTiersBonus(t *testing.T) {
	const short, long = 204.0, 400.0
	const nudge = 0.01
	const tight, loose, none = 0.4, 0.3, 0.2
	tightEdge := acquisition.LengthTight.Allowance(short)
	looseSlackEdge := acquisition.LengthLoose.Allowance(short)
	looseFractionEdge := acquisition.LengthLoose.Allowance(long)
	cases := []struct {
		name  string
		delta float64
		track float64
		want  float64
	}{
		{"exactly the tight allowance long", tightEdge, short, tight},
		{"exactly the tight allowance short", -tightEdge, short, tight},
		{"just inside the tight allowance", tightEdge - nudge, short, tight},
		{"just past the tight allowance", tightEdge + nudge, short, loose},
		{"exactly the loose allowance's slack long", looseSlackEdge, short, loose},
		{"exactly the loose allowance's slack short", -looseSlackEdge, short, loose},
		{"just inside the loose allowance's slack", looseSlackEdge - nudge, short, loose},
		{"just past the loose allowance's slack", looseSlackEdge + nudge, short, none},
		{"exactly the loose allowance's share of a long track long", looseFractionEdge, long, loose},
		{"exactly the loose allowance's share of a long track short", -looseFractionEdge, long, loose},
		{"just inside the loose allowance's share of a long track", looseFractionEdge - nudge, long, loose},
		{"just past the loose allowance's share of a long track", looseFractionEdge + nudge, long, none},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(tc.delta)}
			if got := acquisition.ScoreConfidence(e, tc.track); got != tc.want {
				t.Errorf("ScoreConfidence(%v s off a %v s track) = %v, want %v", tc.delta, tc.track, got, tc.want)
			}
		})
	}
}

func TestScoreConfidence_RanksHardOverSoftOverEveryUnknown(t *testing.T) {
	best := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}, 204)
	hard := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Verdict: "hard", AcoustIDScore: 0.5}, 204)
	soft := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.5}, 204)
	if !(hard > soft && soft >= best) {
		t.Errorf("hard %v, soft %v, best unknown %v: want hard > soft >= unknown", hard, soft, best)
	}
}

type tornerWatch struct{}

func (tornerWatch) Watch(context.Context, string) (acquisition.YtmWatch, error) {
	return acquisition.YtmWatch{
		VideoID: "UeGkPDoLKjI",
		Title:   "Tornerò",
		Artists: []string{"I Santo California"},
		Type:    acquisition.VideoTypeATV,
	}, nil
}

func TestScoreConfidence_ATopicChannelTrackSixSecondsShortClearsTheFloor(t *testing.T) {
	req := acquisition.FindRequest{
		Title:          "Tornerò",
		Artist:         "I Santo California",
		CatalogSources: []acquisition.CatalogSource{{Provider: "youtube", ExternalID: "UeGkPDoLKjI"}},
	}
	found, err := acquisition.NewYtMusicSource(nil, tornerWatch{}).Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("Find = %+v, %v", found, err)
	}
	evidence := acquisition.ConfidenceEvidence{
		Channel:              channelTopic,
		Resolved:             found[0].IsResolved(),
		DurationDeltaSeconds: secs(-6.08),
		SourceTitle:          found[0].Title,
	}

	if got := acquisition.ScoreConfidence(evidence, 245); got < confidenceFloor {
		t.Errorf("confidence = %v, want at least the %v floor", got, confidenceFloor)
	}
}

func secs(v float64) *float64 { return &v }

func TestScoreConfidence_AnUnknownVerdictScoresFromZeroToTheUnknownCap(t *testing.T) {
	lowest := acquisition.ConfidenceEvidence{Channel: channelOther, DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}
	highest := acquisition.ConfidenceEvidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}
	unknownCap := acquisition.ScoreConfidence(acquisition.ConfidenceEvidence{Verdict: "soft", AcoustIDScore: 0.5}, 204)

	if got := acquisition.ScoreConfidence(lowest, 204); got != 0 {
		t.Errorf("lowest unknown score = %v, want 0: a constant change that drops it below 0 needs the zero clamp back", got)
	}
	if got := acquisition.ScoreConfidence(highest, 204); got != unknownCap {
		t.Errorf("highest unknown score = %v, want the unknown cap %v that a weak soft match falls to: a constant change that splits them needs the cap back", got, unknownCap)
	}
}

func storedISRCAndEvidence(t *testing.T, ledger *acquisitiontest.Ledger, id shared.TrackId) (string, service.Evidence) {
	t.Helper()
	got, _ := ledger.Track(id)
	var evidence service.Evidence
	if err := json.Unmarshal(got.Evidence, &evidence); err != nil {
		t.Fatalf("evidence not stored as JSON: %v (%q)", err, got.Evidence)
	}
	if got.ISRC == nil {
		return "", evidence
	}
	return *got.ISRC, evidence
}

func TestExecute_WritesBackOnlyAProvenISRCAndStoresWhereItCameFrom(t *testing.T) {
	const resolvedISRC, recording, catalogLength = "USUG11904206", "blinding-lights-single", 200.0
	catalogISRC, pin := "USUG11904999", recording
	identity := ports.RecordingIdentity{ISRC: resolvedISRC, MBID: recording, MBIDs: []string{recording}}
	doubtedIdentity := identity
	doubtedIdentity.ReferenceDoubted = true
	fingerprinted := func() *identifier {
		return &identifier{match: ports.RecordingMatch{AcoustID: "ac-1", Results: []ports.AcoustIDResult{
			{Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: recording}}},
		}}}
	}
	tests := []struct {
		name        string
		catalogISRC *string
		catalogMBID *string
		identity    ports.RecordingIdentity
		identifier  *identifier
		probed      float64
		wantVerdict string
		wantISRC    string
		wantSource  isrcproof.Source
	}{
		{"the catalog's own ISRC is kept over a proven one", &catalogISRC, nil, identity, fingerprinted(), 0, "hard", catalogISRC, isrcproof.Catalog},
		{"the pinned recording's ISRC", nil, &pin, identity, nil, 0, "", resolvedISRC, isrcproof.PinnedRecording},
		{"a fingerprint-proven ISRC", nil, nil, identity, fingerprinted(), 0, "hard", resolvedISRC, isrcproof.Fingerprint},
		{"a fingerprint the length demotes to soft proves nothing", nil, nil, identity, fingerprinted(), catalogLength + 10, "soft", "", isrcproof.Unproven},
		{"a fingerprint on a doubted reference is only soft, so it proves nothing", nil, nil, doubtedIdentity, fingerprinted(), 0, "soft", "", isrcproof.Unproven},
		{"an unproven resolver ISRC", nil, nil, identity, nil, 0, "", "", isrcproof.Unproven},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := pendingBlindingLights()
			length := catalogLength
			track.ISRC, track.CatalogMBID, track.CatalogDurationSeconds = tt.catalogISRC, tt.catalogMBID, &length
			opts := []func(*service.AcquireTrackAudioService){service.WithRecordingResolver(&resolver{identity: tt.identity})}
			if tt.identifier != nil {
				opts = append(opts, service.WithAudioIdentifier(tt.identifier))
			}
			if tt.probed > 0 {
				opts = append(opts, service.WithAudioProber(lengthProber(tt.probed)))
			}

			ledger, err := acquireWith(t, track, freshAudioSource{}, opts...)

			requireReady(t, ledger, track.ID, err)
			isrc, evidence := storedISRCAndEvidence(t, ledger, track.ID)
			if evidence.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %q, want %q: the row must reach the path it names", evidence.Verdict, tt.wantVerdict)
			}
			if isrc != tt.wantISRC || evidence.ISRCSource != tt.wantSource {
				t.Errorf("stored ISRC %q with isrc_source %q, want %q with %q", isrc, evidence.ISRCSource, tt.wantISRC, tt.wantSource)
			}
		})
	}
}

func TestExecute_DrinkingInLAWritesNoGuessedISRC(t *testing.T) {
	const (
		drinkingISRC  = "CAA509814003"
		drinkingAlbum = "5d6efd30-23c6-4a9b-85af-706ea2df9022"
		drinkingRemix = "d3dc2825-20c0-4a4f-a963-baa589049146"
	)
	recorded := recordingresolver.NewRecorded(
		[]ports.SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ISRC: drinkingISRC, MBID: drinkingRemix, Duration: 236}},
		[]ports.ISRCRecording{{MBID: drinkingAlbum, Duration: 236}}, nil)
	track := pendingBlindingLights()
	catalogLength := 236.0
	track.Title, track.Artist, track.Album, track.CatalogDurationSeconds = "Drinking in L.A.", "Bran Van 3000", "Glee", &catalogLength
	source := &offeringSource{found: []ports.AudioCandidate{{
		Title: "Drinking in L.A.", URL: "https://www.youtube.com/watch?v=-_5yNLsZ5Pk", Channel: "Bran Van 3000 - Topic", Categories: []string{"Music"},
	}}}
	var resolved ports.RecordingIdentity
	observe := service.WithContextObserver(func(ac *service.AcquisitionContext) { resolved = ac.Identity })

	ledger, err := acquireWith(t, track, source, service.WithRecordingResolver(recorded), observe)

	requireReady(t, ledger, track.ID, err)
	if resolved.ISRC != drinkingISRC {
		t.Fatalf("resolver identity ISRC = %q, want the search hit's guess %q", resolved.ISRC, drinkingISRC)
	}
	if isrc, evidence := storedISRCAndEvidence(t, ledger, track.ID); isrc != "" || evidence.ISRCSource != isrcproof.Unproven {
		t.Errorf("stored ISRC %q with isrc_source %q, want none with %q: the search hit's ISRC was never proven", isrc, evidence.ISRCSource, isrcproof.Unproven)
	}
}
