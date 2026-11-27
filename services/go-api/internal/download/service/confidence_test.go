package service_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/isrcwriteback"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/musicbrainzmatch"
	"altune/go-api/internal/shared/ids"
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
		kind    ports.CandidateKind
		omv     bool
		channel string
		want    audioquality.SourceKind
	}{
		{"art track", ports.KindArtTrack, false, channelOther, audioquality.ArtTrack},
		{"observed official video", ports.KindPinnedVideo, true, channelOther, audioquality.OfficialVideo},
		{"pinned video of an unknown type is not official", ports.KindPinnedVideo, false, channelTopic, audioquality.FanUpload},
		{"upload from the artist's channel", ports.KindUpload, false, channelArtist, audioquality.LabelUpload},
		{"upload from a topic channel", ports.KindUpload, false, channelTopic, audioquality.LabelUpload},
		{"upload from any other channel", ports.KindUpload, false, channelOther, audioquality.FanUpload},
		{"catalog pin is not assumed official", ports.KindCatalogPin, false, channelTopic, audioquality.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := service.SourceKindOf(downloadtest.Candidate("", downloadtest.WithKind(tc.kind), downloadtest.WithObservedOMV(tc.omv)), tc.channel); got != tc.want {
				t.Errorf("SourceKindOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUploaderSourceKind(t *testing.T) {
	cases := []struct {
		name, artist, channel string
		want                  audioquality.SourceKind
	}{
		{"the artist's own channel", "Daft Punk", "Daft Punk", audioquality.LabelUpload},
		{"a topic channel", "Daft Punk", "Daft Punk - Topic", audioquality.LabelUpload},
		{"someone else's channel", "Daft Punk", "lyrics hub", audioquality.FanUpload},
		{"no channel", "Daft Punk", "", audioquality.FanUpload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := service.UploaderSourceKind(tc.artist, tc.channel); got != tc.want {
				t.Errorf("UploaderSourceKind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFingerprintOf(t *testing.T) {
	cases := map[service.VerdictKind]audioquality.FingerprintResult{
		service.VerdictHard:          audioquality.Matches,
		service.VerdictSoft:          audioquality.Approximates,
		service.VerdictOtherVersion:  audioquality.Contradicts,
		service.VerdictDifferentSong: audioquality.Unsupported,
		service.VerdictUnknown:       audioquality.Silent,
		"":                           audioquality.Silent,
	}
	for verdict, want := range cases {
		if got := service.FingerprintOf(verdict); got != want {
			t.Errorf("FingerprintOf(%q) = %v, want %v", verdict, got, want)
		}
	}
}

func TestScoreConfidence(t *testing.T) {
	tests := []struct {
		name  string
		e     service.Evidence
		track float64
		want  float64
	}{
		{"hard", service.Evidence{Verdict: "hard", AcoustIDScore: 0.97}, 204, 0.95},
		{"soft", service.Evidence{Verdict: "soft", AcoustIDScore: 0.95}, 204, 0.85},
		{"hard with a low score", service.Evidence{Verdict: "hard", AcoustIDScore: 0.8}, 204, 0.85},
		{"soft with a low score stays at the unknown cap", service.Evidence{Verdict: "soft", AcoustIDScore: 0.8}, 204, 0.8},
		{"hard at exactly the low-score cutoff keeps full confidence", service.Evidence{Verdict: "hard", AcoustIDScore: 0.92}, 204, 0.95},
		{"hard with no score recorded", service.Evidence{Verdict: "hard"}, 204, 0.95},
		{"topic one second off", service.Evidence{Verdict: "unknown", Channel: channelTopic, DurationDeltaSeconds: secs(1)}, 204, 0.7},
		{"other channel twenty seconds off", service.Evidence{Verdict: "unknown", Channel: channelOther, DurationDeltaSeconds: secs(20)}, 204, 0.2},
		{"artist channel on the spot", service.Evidence{Verdict: "unknown", Channel: channelArtist, DurationDeltaSeconds: secs(0)}, 204, 0.7},
		{"no fingerprint run", service.Evidence{Channel: channelTopic, DurationDeltaSeconds: secs(-1)}, 204, 0.7},
		{"topic twenty seconds short earns no length bonus", service.Evidence{Channel: channelTopic, DurationDeltaSeconds: secs(-20)}, 204, 0.5},
		{"loose duration window", service.Evidence{Channel: channelOther, DurationDeltaSeconds: secs(10)}, 204, 0.3},
		{"loose window grows with the track", service.Evidence{Channel: channelOther, DurationDeltaSeconds: secs(20)}, 400, 0.3},
		{"qualifier costs 0.2", service.Evidence{Channel: channelTopic, DurationDeltaSeconds: secs(0), Qualifiers: []string{"radio edit"}}, 204, 0.5},
		{"resolved adds 0.1 and the cap holds", service.Evidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}, 204, 0.8},
		{"unknown length earns no length bonus", service.Evidence{Verdict: "unknown", Channel: channelTopic}, 204, 0.5},
		{"never below zero", service.Evidence{Channel: channelOther, DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}, 204, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.ScoreConfidence(tt.e, tt.track); got != tt.want {
				t.Errorf("ScoreConfidence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreConfidence_ALengthExactlyAtEachToleranceEarnsThatTiersBonus(t *testing.T) {
	const short, long = 204.0, 400.0
	const nudge = 0.01
	const tight, loose, none = 0.4, 0.3, 0.2
	tightEdge := domain.LengthTight.Allowance(short)
	looseSlackEdge := domain.LengthLoose.Allowance(short)
	looseFractionEdge := domain.LengthLoose.Allowance(long)
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
			e := service.Evidence{Channel: channelOther, DurationDeltaSeconds: secs(tc.delta)}
			if got := service.ScoreConfidence(e, tc.track); got != tc.want {
				t.Errorf("ScoreConfidence(%v s off a %v s track) = %v, want %v", tc.delta, tc.track, got, tc.want)
			}
		})
	}
}

func TestScoreConfidence_RanksHardOverSoftOverEveryUnknown(t *testing.T) {
	best := service.ScoreConfidence(service.Evidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}, 204)
	hard := service.ScoreConfidence(service.Evidence{Verdict: "hard", AcoustIDScore: 0.5}, 204)
	soft := service.ScoreConfidence(service.Evidence{Verdict: "soft", AcoustIDScore: 0.5}, 204)
	if !(hard > soft && soft >= best) {
		t.Errorf("hard %v, soft %v, best unknown %v: want hard > soft >= unknown", hard, soft, best)
	}
}

type tornerWatch struct{}

func (tornerWatch) Watch(context.Context, string) (ytmsearch.Watch, error) {
	return ytmsearch.Watch{
		VideoID: "UeGkPDoLKjI",
		Title:   "Tornerò",
		Artists: []string{"I Santo California"},
		Type:    download.VideoTypeAudioOnly,
	}, nil
}

func TestScoreConfidence_ATopicChannelTrackSixSecondsShortClearsTheFloor(t *testing.T) {
	req := download.FindRequest{
		Title:          "Tornerò",
		Artist:         "I Santo California",
		CatalogSources: []download.CatalogSource{{Provider: "youtube", ExternalID: "UeGkPDoLKjI"}},
	}
	found, err := download.NewYtMusicSource(nil, tornerWatch{}).Find(context.Background(), req)
	if err != nil || len(found) != 1 {
		t.Fatalf("Find = %+v, %v", found, err)
	}
	evidence := service.Evidence{
		Channel:              channelTopic,
		Resolved:             found[0].IsResolved(),
		DurationDeltaSeconds: secs(-6.08),
		SourceTitle:          found[0].Title,
	}

	if got := service.ScoreConfidence(evidence, 245); got < confidenceFloor {
		t.Errorf("confidence = %v, want at least the %v floor", got, confidenceFloor)
	}
}

func secs(v float64) *float64 { return &v }

func TestScoreConfidence_AnUnknownVerdictScoresFromZeroToTheUnknownCap(t *testing.T) {
	lowest := service.Evidence{Channel: channelOther, DurationDeltaSeconds: secs(90), Qualifiers: []string{"edit"}}
	highest := service.Evidence{Channel: channelTopic, Resolved: true, DurationDeltaSeconds: secs(0)}
	unknownCap := service.ScoreConfidence(service.Evidence{Verdict: "soft", AcoustIDScore: 0.5}, 204)

	if got := service.ScoreConfidence(lowest, 204); got != 0 {
		t.Errorf("lowest unknown score = %v, want 0: a constant change that drops it below 0 needs the zero clamp back", got)
	}
	if got := service.ScoreConfidence(highest, 204); got != unknownCap {
		t.Errorf("highest unknown score = %v, want the unknown cap %v that a weak soft match falls to: a constant change that splits them needs the cap back", got, unknownCap)
	}
}

func storedISRCAndEvidence(t *testing.T, ledger *inmemoryledger.Ledger, id ids.TrackId) (string, service.Evidence) {
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
		wantSource  isrcwriteback.Source
	}{
		{"the catalog's own ISRC is kept over a proven one", &catalogISRC, nil, identity, fingerprinted(), 0, "hard", catalogISRC, isrcwriteback.Catalog},
		{"the pinned recording's ISRC", nil, &pin, identity, nil, 0, "", resolvedISRC, isrcwriteback.PinnedRecording},
		{"a fingerprint-proven ISRC", nil, nil, identity, fingerprinted(), 0, "hard", resolvedISRC, isrcwriteback.Fingerprint},
		{"a fingerprint the length demotes to soft proves nothing", nil, nil, identity, fingerprinted(), catalogLength + 10, "soft", "", isrcwriteback.Unproven},
		{"a fingerprint on a doubted reference is only soft, so it proves nothing", nil, nil, doubtedIdentity, fingerprinted(), 0, "soft", "", isrcwriteback.Unproven},
		{"an unproven resolver ISRC", nil, nil, identity, nil, 0, "", "", isrcwriteback.Unproven},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := pendingBlindingLights()
			length := catalogLength
			track.ISRC, track.CatalogMBID, track.CatalogDurationSeconds = tt.catalogISRC, tt.catalogMBID, &length
			wiring := noVerification()
			if tt.identifier != nil {
				wiring = verifiedBy(tt.identifier)
			}
			wiring = resolving(&resolver{identity: tt.identity}, wiring)
			stages := noStages()
			if tt.probed > 0 {
				stages = noStages(probedBy(lengthProber(tt.probed)))
			}

			ledger, err := acquireVerified(t, track, freshAudioSource{}, wiring, stages)

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
	recorded := musicbrainzmatch.NewRecorded(
		[]ports.SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ISRC: drinkingISRC, MBID: drinkingRemix, Duration: 236}},
		[]ports.ISRCRecording{{MBID: drinkingAlbum, Duration: 236}}, nil)
	track := pendingBlindingLights()
	catalogLength := 236.0
	track.Title, track.Artist, track.Album, track.CatalogDurationSeconds = "Drinking in L.A.", "Bran Van 3000", "Glee", &catalogLength
	source := &offeringSource{found: []ports.AudioCandidate{downloadtest.Candidate(
		"https://www.youtube.com/watch?v=-_5yNLsZ5Pk",
		downloadtest.WithTitle("Drinking in L.A."),
		downloadtest.WithChannel("Bran Van 3000 - Topic"),
		downloadtest.WithCategories([]string{"Music"}),
	)}}
	var resolved ports.RecordingIdentity
	stages := noStages()
	stages.Observer = func(ac *service.AcquisitionContext) { resolved = ac.Identity }

	ledger, err := acquireVerified(t, track, source, resolving(recorded, noVerification()), stages)

	requireReady(t, ledger, track.ID, err)
	if resolved.ISRC != drinkingISRC {
		t.Fatalf("resolver identity ISRC = %q, want the search hit's guess %q", resolved.ISRC, drinkingISRC)
	}
	if isrc, evidence := storedISRCAndEvidence(t, ledger, track.ID); isrc != "" || evidence.ISRCSource != isrcwriteback.Unproven {
		t.Errorf("stored ISRC %q with isrc_source %q, want none with %q: the search hit's ISRC was never proven", isrc, evidence.ISRCSource, isrcwriteback.Unproven)
	}
}
