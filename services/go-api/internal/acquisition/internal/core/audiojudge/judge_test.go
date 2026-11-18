package audiojudge_test

import (
	"altune/go-api/internal/acquisition"
	"testing"
)

var productionCuts = acquisition.JudgeCutsWithProvenMax(0.10)

func linked(mbid, title string, length float64, artists ...string) acquisition.AcoustIDResult {
	return acquisition.AcoustIDResult{ID: "aid-" + mbid, Score: 0.97, Recordings: []acquisition.LinkedRecording{{MBID: mbid, Title: title, Artists: artists, Duration: length}}}
}

func preview(rate float64) *acquisition.JudgePreview {
	return &acquisition.JudgePreview{BitErrorRate: rate, OverlapSeconds: 29}
}

func TestJudge_Table(t *testing.T) {
	identity := acquisition.JudgeIdentity{
		Version: "v1", ISRC: "USRC10000001", MBID: "mb-bound", Title: "Song", Artist: "Artist",
		CatalogLengths: map[string]float64{"deezer": 200, "musicbrainz": 201},
	}
	noISRC := identity
	noISRC.ISRC = ""
	art := acquisition.JudgeSource{Kind: acquisition.ArtTrack}
	fan := acquisition.JudgeSource{Kind: acquisition.FanUpload}

	cases := []struct {
		name      string
		identity  acquisition.JudgeIdentity
		audio     acquisition.JudgeAudio
		source    acquisition.JudgeSource
		candidate string
		kind      acquisition.JudgeKind
		grade     acquisition.Grade
	}{
		{"acoustid links the bound recording with matching length", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, art, "Song", acquisition.JudgeProven, acquisition.GradeA},
		{"acoustid proof on a fan upload grades B", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, fan, "Song", acquisition.JudgeProven, acquisition.GradeB},
		{"acoustid links the bound recording at a length 100s off is no answer", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 300, "Artist")}, Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeC},
		{"preview at the A cut on an official source with matching tags", identity, acquisition.JudgeAudio{Preview: preview(0.10), Length: 200}, art, "Song", acquisition.JudgeProven, acquisition.GradeA},
		{"preview at the A cut on a fan upload is only likely", identity, acquisition.JudgeAudio{Preview: preview(0.10), Length: 200}, fan, "Song", acquisition.JudgeLikely, acquisition.GradeB},
		{"preview at the A cut but the version tags differ is only likely", identity, acquisition.JudgeAudio{Preview: preview(0.05), Length: 200}, art, "Song (Live)", acquisition.JudgeLikely, acquisition.GradeB},
		{"preview at 0.18 is likely", identity, acquisition.JudgeAudio{Preview: preview(0.18), Length: 200}, art, "Song", acquisition.JudgeLikely, acquisition.GradeB},
		{"preview at 0.19 proves nothing", identity, acquisition.JudgeAudio{Preview: preview(0.19), Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeC},
		{"preview overlap too short is no preview", identity, acquisition.JudgeAudio{Preview: &acquisition.JudgePreview{BitErrorRate: 0.02, OverlapSeconds: 5}, Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeC},
		{"same-title acoustid duplicate with no ISRC is likely", noISRC, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Length: 200}, art, "Song", acquisition.JudgeLikely, acquisition.GradeB},
		{"same-title acoustid duplicate with an ISRC is another recording, one source, unknown", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeD},
		{"same-title duplicate but the preview is absent is not likely", noISRC, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Preview: preview(0.40), Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeC},
		{"no catalog identity is unknown and best available, whatever the audio says", acquisition.JudgeIdentity{Version: "v1", Title: "Song", Artist: "Artist"}, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.45), Length: 200}, fan, "Song (Live)", acquisition.JudgeUnknown, acquisition.GradeBestAvailable},
		{"nothing to go on is unknown", identity, acquisition.JudgeAudio{}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeD},
		{"a part number alone never contradicts", identity, acquisition.JudgeAudio{Length: 200}, art, "Song pt 2", acquisition.JudgeUnknown, acquisition.GradeC},
		{"a preview that is absent alone never contradicts", identity, acquisition.JudgeAudio{Preview: preview(0.45), Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeC},
		{"another acoustid recording alone never contradicts", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Length: 200}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeD},
		{"another acoustid recording and an absent preview contradict", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.40), Length: 200}, art, "Song", acquisition.JudgeContradicted, acquisition.GradeF},
		{"a different version tag and a length outside the window contradict", identity, acquisition.JudgeAudio{Length: 260}, art, "Song (Live)", acquisition.JudgeContradicted, acquisition.GradeF},
		{"a length outside the window alone never contradicts", identity, acquisition.JudgeAudio{Length: 260}, art, "Song", acquisition.JudgeUnknown, acquisition.GradeD},
		{"a matching preview vetoes two sources against", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.12), Length: 200}, fan, "Song", acquisition.JudgeLikely, acquisition.GradeB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := acquisition.Judge(acquisition.JudgeInput{Identity: tc.identity, Audio: tc.audio, Source: tc.source, CandidateTitle: tc.candidate}, productionCuts)
			if got.Kind != tc.kind || got.Grade != tc.grade {
				t.Errorf("verdict = %s/%s, want %s/%s; reasons: %v", got.Kind, got.Grade, tc.kind, tc.grade, got.Reasons)
			}
			if len(got.Reasons) == 0 {
				t.Error("a verdict must say why")
			}
			if got.JudgedAgainst != tc.identity.Version {
				t.Errorf("JudgedAgainst = %q, want %q", got.JudgedAgainst, tc.identity.Version)
			}
		})
	}
}

func TestJudge_Goldens(t *testing.T) {
	art := acquisition.JudgeSource{Kind: acquisition.ArtTrack}
	fan := acquisition.JudgeSource{Kind: acquisition.FanUpload}

	t.Run("KANKAN Nun To Me 2: no MB part 2, acoustid names part 1, the preview matches part 2", func(t *testing.T) {
		identity := acquisition.JudgeIdentity{Version: "v1", ISRC: "JPPO02000002", Title: "Nun To Me 2", Artist: "KANKAN", CatalogLengths: map[string]float64{"deezer": 188}}
		audio := acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-part1", "Nun To Me", 188, "KANKAN")}, Preview: preview(0.08), Length: 188}

		official := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio, Source: art, CandidateTitle: "Nun To Me 2"}, productionCuts)
		if official.Kind != acquisition.JudgeProven || official.Grade != acquisition.GradeA {
			t.Errorf("official upload = %s/%s, want proven/A; reasons: %v", official.Kind, official.Grade, official.Reasons)
		}
		reupload := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "Nun To Me 2"}, productionCuts)
		if reupload.Kind != acquisition.JudgeLikely {
			t.Errorf("fan re-upload = %s, want likely; reasons: %v", reupload.Kind, reupload.Reasons)
		}
	})

	t.Run("Autumn: a stale rejection expires when the identity is re-pinned", func(t *testing.T) {
		wrongPin := acquisition.JudgeIdentity{Version: "pin-1", ISRC: "USAT20000001", Title: "Still The Same!", Artist: "Autumn!", CatalogLengths: map[string]float64{"deezer": 201}}
		audio := acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-part1", "Still The Same!", 201, "Autumn!")}, Preview: preview(0.40), Length: 201}
		rejected := acquisition.Judge(acquisition.JudgeInput{Identity: wrongPin, Audio: audio, Source: art, CandidateTitle: "Still The Same! 2"}, productionCuts)
		if rejected.Kind != acquisition.JudgeContradicted {
			t.Fatalf("against the wrong pin = %s, want contradicted; reasons: %v", rejected.Kind, rejected.Reasons)
		}
		if !rejected.HoldsFor("pin-1") || rejected.HoldsFor("pin-2") {
			t.Error("a verdict holds only for the identity version it was judged against")
		}

		repinned := wrongPin
		repinned.Version, repinned.Title = "pin-2", "Still The Same! 2"
		audio.Preview = preview(0.06)
		rejudged := acquisition.Judge(acquisition.JudgeInput{Identity: repinned, Audio: audio, Source: art, CandidateTitle: "Still The Same! 2"}, productionCuts)
		if rejudged.Kind != acquisition.JudgeProven || rejudged.JudgedAgainst != "pin-2" {
			t.Errorf("after the re-pin = %s against %q, want proven against pin-2; reasons: %v", rejudged.Kind, rejudged.JudgedAgainst, rejudged.Reasons)
		}
	})

	t.Run("Platinum Blonde Live: catalog lengths disagree and the preview matches", func(t *testing.T) {
		identity := acquisition.JudgeIdentity{
			Version: "v1", ISRC: "ITA000000001", Title: "Live at City Hall", Artist: "Platinum Blonde",
			CatalogLengths: map[string]float64{"deezer": 251, "musicbrainz": 297},
		}
		audio := acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-studio", "City Hall", 297, "Platinum Blonde")}, Preview: preview(0.06), Length: 297}
		for source, want := range map[acquisition.JudgeSource]acquisition.JudgeKind{art: acquisition.JudgeProven, fan: acquisition.JudgeLikely} {
			got := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio, Source: source, CandidateTitle: "Live at City Hall"}, productionCuts)
			if got.Kind != want {
				t.Errorf("%s source = %s, want %s; reasons: %v", source.Kind, got.Kind, want, got.Reasons)
			}
		}
		withoutPreview := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: acquisition.JudgeAudio{Length: 297}, Source: art, CandidateTitle: "Live at City Hall"}, productionCuts)
		if withoutPreview.Kind != acquisition.JudgeUnknown || withoutPreview.Grade != acquisition.GradeC {
			t.Errorf("no preview = %s/%s, want unknown/C", withoutPreview.Kind, withoutPreview.Grade)
		}
	})

	t.Run("Pavarotti: acoustid names another performance and the preview is absent", func(t *testing.T) {
		identity := acquisition.JudgeIdentity{Version: "v1", ISRC: "ITB000000001", MBID: "mb-studio", Title: "Nessun Dorma", Artist: "Luciano Pavarotti", CatalogLengths: map[string]float64{"deezer": 195}}
		audio := acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-live", "Nessun Dorma (Live)", 195, "Luciano Pavarotti")}, Preview: preview(0.45), Length: 195}
		got := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "Nessun Dorma"}, productionCuts)
		if got.Kind != acquisition.JudgeContradicted || got.Grade != acquisition.GradeF {
			t.Errorf("verdict = %s/%s, want contradicted/F; reasons: %v", got.Kind, got.Grade, got.Reasons)
		}
	})

	t.Run("Travis Scott outside: another song with the same title, AcoustID and length alone", func(t *testing.T) {
		identity := acquisition.JudgeIdentity{Version: "v1", ISRC: "USSM10000001", MBID: "mb-ts", Title: "outside", Artist: "Travis Scott", CatalogLengths: map[string]float64{"deezer": 241}}
		audio := acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-other", "Outside", 215, "Another Artist")}, Length: 215}
		got := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "outside"}, productionCuts)
		if got.Kind != acquisition.JudgeContradicted {
			t.Errorf("verdict = %s, want contradicted; reasons: %v", got.Kind, got.Reasons)
		}
	})

	t.Run("unreleased track: no catalog identity is never contradicted, whatever the audio says", func(t *testing.T) {
		unreleased := acquisition.JudgeIdentity{Version: "v1", Title: "Leaked Song", Artist: "Artist"}
		other := []acquisition.AcoustIDResult{linked("mb-other", "Another Song", 200, "Someone")}
		answers := map[string]acquisition.JudgeAudio{
			"an acoustid answer and a differing tag": {AcoustID: other, Length: 200},
			"a preview that is absent":               {Preview: preview(0.45), Length: 200},
			"both together":                          {AcoustID: other, Preview: preview(0.45), Length: 200},
			"nothing":                                {},
		}
		for name, audio := range answers {
			got := acquisition.Judge(acquisition.JudgeInput{Identity: unreleased, Audio: audio, Source: fan, CandidateTitle: "Leaked Song (Live)"}, productionCuts)
			if got.Kind != acquisition.JudgeUnknown || got.Grade != acquisition.GradeBestAvailable || len(got.Reasons) == 0 || got.JudgedAgainst != "v1" {
				t.Errorf("%s = %s/%s %v, want unknown/best_available with a reason", name, got.Kind, got.Grade, got.Reasons)
			}
		}
	})
}

func TestJudge_CallerFacts(t *testing.T) {
	art := acquisition.JudgeSource{Kind: acquisition.ArtTrack}
	identity := acquisition.JudgeIdentity{
		Version: "v1", ISRC: "USRC10000001", MBID: "mb-bound", MergedMBIDs: []string{"mb-merged"}, Title: "Song", Artist: "Artist",
		CatalogLengths: map[string]float64{"deezer": 200},
	}
	noISRC := identity
	noISRC.ISRC = ""
	dup := []acquisition.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}

	cases := []struct {
		name      string
		identity  acquisition.JudgeIdentity
		audio     acquisition.JudgeAudio
		wantKind  acquisition.JudgeKind
		wantLower bool
	}{
		{"a merged MBID is the bound recording", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-merged", "Song", 200, "Artist")}, Length: 200}, acquisition.JudgeProven, false},
		{"a link with no MBID names nothing, so it neither proves nor contradicts", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("", "Other Song", 200, "Someone")}, Preview: preview(0.40), Length: 200}, acquisition.JudgeUnknown, false},
		{"links that carry no title name nothing", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-untitled", "", 200, "Someone")}, Preview: preview(0.40), Length: 200}, acquisition.JudgeUnknown, false},
		{"a recording whose looked-up ISRCs include the bound one is the bound recording", identity, acquisition.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]acquisition.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", ISRCs: []string{"usrc10000001"}}}}, acquisition.JudgeProven, false},
		{"a looked-up recording with another ISRC is another recording", identity, acquisition.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]acquisition.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", ISRCs: []string{"USRC19999999"}}}}, acquisition.JudgeUnknown, false},
		{"a looked-up duplicate with no ISRC and the same version is likely", identity, acquisition.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]acquisition.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song"}}}, acquisition.JudgeLikely, false},
		{"a looked-up duplicate whose disambiguation says live is another version", noISRC, acquisition.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]acquisition.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", Disambiguation: "live"}}}, acquisition.JudgeUnknown, false},
		{"a same-title link whose version cannot be looked up is not a duplicate when lookups are required", noISRC, acquisition.JudgeAudio{AcoustID: dup, Length: 200, RequireVersionFacts: true}, acquisition.JudgeUnknown, false},
		{"a same-title link is a duplicate by its own title when lookups are not required", noISRC, acquisition.JudgeAudio{AcoustID: dup, Length: 200}, acquisition.JudgeLikely, false},
		{"most surviving links naming other songs outvote one same-title duplicate", noISRC, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{
			linked("mb-dup", "Song", 200, "Artist"), linked("mb-a", "Other A", 200, "Someone"), linked("mb-b", "Other B", 200, "Someone"),
		}, Preview: preview(0.40), Length: 200}, acquisition.JudgeContradicted, true},
		{"a bound link with a length outside every catalog length is only likely and lowers a stored grade", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: 262}, acquisition.JudgeLikely, true},
		{"a bound link inside the catalog length is proven and lowers nothing", identity, acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: 201}, acquisition.JudgeProven, false},
		{"a length outside every catalog length with no fingerprint answer lowers nothing", identity, acquisition.JudgeAudio{Length: 262}, acquisition.JudgeUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := acquisition.Judge(acquisition.JudgeInput{Identity: tc.identity, Audio: tc.audio, Source: art, CandidateTitle: "Song"}, productionCuts)
			if got.Kind != tc.wantKind || got.LowersStoredGrade != tc.wantLower {
				t.Errorf("verdict = %s lowers=%v, want %s lowers=%v; reasons: %v", got.Kind, got.LowersStoredGrade, tc.wantKind, tc.wantLower, got.Reasons)
			}
		})
	}
}

func TestJudge_ALengthContradictionLowersTheGradeBelowA(t *testing.T) {
	identity := acquisition.JudgeIdentity{Version: "v1", MBID: "mb-bound", Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}
	audio := func(length float64) acquisition.JudgeAudio {
		return acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: length}
	}

	fits := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio(200), Source: acquisition.JudgeSource{Kind: acquisition.ArtTrack}, CandidateTitle: "Song"}, productionCuts)
	wrongLength := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: audio(262), Source: acquisition.JudgeSource{Kind: acquisition.ArtTrack}, CandidateTitle: "Song"}, productionCuts)

	if fits.Grade != acquisition.GradeA || wrongLength.Grade != acquisition.GradeB || !wrongLength.Grade.IsKnown() {
		t.Errorf("grades = %s and %s, want A for the fitting file and B for the wrong-length one", fits.Grade, wrongLength.Grade)
	}
}

func TestJudge_ADoubtedReferenceCapsAFingerprintProofAtLikely(t *testing.T) {
	identity := acquisition.JudgeIdentity{Version: "v1", MBID: "mb-bound", Doubted: true, Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}

	byAcoustID := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: acquisition.JudgeAudio{AcoustID: []acquisition.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, Source: acquisition.JudgeSource{Kind: acquisition.ArtTrack}, CandidateTitle: "Song"}, productionCuts)
	byPreview := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: acquisition.JudgeAudio{Preview: preview(0.05), Length: 200}, Source: acquisition.JudgeSource{Kind: acquisition.ArtTrack}, CandidateTitle: "Song"}, productionCuts)

	if byAcoustID.Kind != acquisition.JudgeLikely || byPreview.Kind != acquisition.JudgeLikely || byAcoustID.LowersStoredGrade {
		t.Errorf("verdicts = %s and %s (lowers %v), want likely for both and no lowering", byAcoustID.Kind, byPreview.Kind, byAcoustID.LowersStoredGrade)
	}
}

func TestJudge_AUserUploadIsProvenByAPreviewAtTheCutWhateverItsSource(t *testing.T) {
	identity := acquisition.JudgeIdentity{Version: "v1", ISRC: "USRC10000001", Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}

	got := acquisition.Judge(acquisition.JudgeInput{Identity: identity, Audio: acquisition.JudgeAudio{Preview: preview(0.08), Length: 200}, Source: acquisition.JudgeSource{Kind: acquisition.UserUpload}}, productionCuts)

	if got.Kind != acquisition.JudgeProven {
		t.Errorf("verdict = %s, want proven: a user's own file needs no official source", got.Kind)
	}
}

func TestCutsWithProvenMax_TheProvenCutComesFromTheCallerAndNeverExceedsTheLikelyCut(t *testing.T) {
	loose := acquisition.JudgeCutsWithProvenMax(0.25)
	strict := acquisition.JudgeCutsWithProvenMax(0.05)

	if loose.ProvenMax != 0.25 || loose.LikelyMax < loose.ProvenMax {
		t.Errorf("loose cuts = %+v, want a proven cut of 0.25 under the likely cut", loose)
	}
	if strict.ProvenMax != 0.05 || strict.LikelyMax != productionCuts.LikelyMax || strict.AbsentMin != productionCuts.AbsentMin {
		t.Errorf("strict cuts = %+v, want only the proven cut changed", strict)
	}
}
