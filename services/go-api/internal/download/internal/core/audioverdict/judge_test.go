package audioverdict_test

import (
	"altune/go-api/internal/download"
	"testing"
)

var productionCuts = download.JudgeCutsWithProvenMax(0.10)

func linked(mbid, title string, length float64, artists ...string) download.AcoustIDResult {
	return download.AcoustIDResult{ID: "aid-" + mbid, Score: 0.97, Recordings: []download.LinkedRecording{{MBID: mbid, Title: title, Artists: artists, Duration: length}}}
}

func preview(rate float64) *download.JudgePreview {
	return &download.JudgePreview{BitErrorRate: rate, OverlapSeconds: 29}
}

func TestJudge_Table(t *testing.T) {
	identity := download.JudgeIdentity{
		Version: "v1", ISRC: "USRC10000001", MBID: "mb-bound", Title: "Song", Artist: "Artist",
		CatalogLengths: map[string]float64{"deezer": 200, "musicbrainz": 201},
	}
	noISRC := identity
	noISRC.ISRC = ""
	art := download.JudgeSource{Kind: download.ArtTrack}
	fan := download.JudgeSource{Kind: download.FanUpload}

	cases := []struct {
		name      string
		identity  download.JudgeIdentity
		audio     download.JudgeAudio
		source    download.JudgeSource
		candidate string
		kind      download.JudgeKind
		grade     download.Grade
	}{
		{"acoustid links the bound recording with matching length", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, art, "Song", download.JudgeProven, download.GradeA},
		{"acoustid links the bound recording when the audio's length is unknown", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 0}, art, "Song", download.JudgeProven, download.GradeA},
		{"acoustid proof on a fan upload grades B", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, fan, "Song", download.JudgeProven, download.GradeB},
		{"acoustid links the bound recording at a length 100s off is no answer", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 300, "Artist")}, Length: 200}, art, "Song", download.JudgeUnknown, download.GradeC},
		{"preview at the A cut on an official source with matching tags", identity, download.JudgeAudio{Preview: preview(0.10), Length: 200}, art, "Song", download.JudgeProven, download.GradeA},
		{"preview at the A cut on a fan upload is only likely", identity, download.JudgeAudio{Preview: preview(0.10), Length: 200}, fan, "Song", download.JudgeLikely, download.GradeB},
		{"preview at the A cut but the version tags differ is only likely", identity, download.JudgeAudio{Preview: preview(0.05), Length: 200}, art, "Song (Live)", download.JudgeLikely, download.GradeB},
		{"preview at 0.18 is likely", identity, download.JudgeAudio{Preview: preview(0.18), Length: 200}, art, "Song", download.JudgeLikely, download.GradeB},
		{"preview at 0.19 proves nothing", identity, download.JudgeAudio{Preview: preview(0.19), Length: 200}, art, "Song", download.JudgeUnknown, download.GradeC},
		{"preview overlap too short is no preview", identity, download.JudgeAudio{Preview: &download.JudgePreview{BitErrorRate: 0.02, OverlapSeconds: 5}, Length: 200}, art, "Song", download.JudgeUnknown, download.GradeC},
		{"same-title acoustid duplicate with no ISRC is likely", noISRC, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Length: 200}, art, "Song", download.JudgeLikely, download.GradeB},
		{"same-title acoustid duplicate with an ISRC is another recording, one source, unknown", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Length: 200}, art, "Song", download.JudgeUnknown, download.GradeD},
		{"same-title duplicate but the preview is absent is not likely", noISRC, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Preview: preview(0.40), Length: 200}, art, "Song", download.JudgeUnknown, download.GradeC},
		{"no catalog identity is unknown and best available, whatever the audio says", download.JudgeIdentity{Version: "v1", Title: "Song", Artist: "Artist"}, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.45), Length: 200}, fan, "Song (Live)", download.JudgeUnknown, download.GradeBestAvailable},
		{"nothing to go on is unknown", identity, download.JudgeAudio{}, art, "Song", download.JudgeUnknown, download.GradeD},
		{"a part number alone never contradicts", identity, download.JudgeAudio{Length: 200}, art, "Song pt 2", download.JudgeUnknown, download.GradeC},
		{"a preview that is absent alone never contradicts", identity, download.JudgeAudio{Preview: preview(0.45), Length: 200}, art, "Song", download.JudgeUnknown, download.GradeC},
		{"another acoustid recording alone never contradicts", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Length: 200}, art, "Song", download.JudgeUnknown, download.GradeD},
		{"another acoustid recording and an absent preview contradict", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.40), Length: 200}, art, "Song", download.JudgeContradicted, download.GradeF},
		{"a different version tag and a length outside the window contradict", identity, download.JudgeAudio{Length: 260}, art, "Song (Live)", download.JudgeContradicted, download.GradeF},
		{"a length outside the window alone never contradicts", identity, download.JudgeAudio{Length: 260}, art, "Song", download.JudgeUnknown, download.GradeD},
		{"a matching preview vetoes two sources against", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.12), Length: 200}, fan, "Song", download.JudgeLikely, download.GradeB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := download.Judge(download.JudgeInput{Identity: tc.identity, Audio: tc.audio, Source: tc.source, CandidateTitle: tc.candidate}, productionCuts)
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
	art := download.JudgeSource{Kind: download.ArtTrack}
	fan := download.JudgeSource{Kind: download.FanUpload}

	t.Run("KANKAN Nun To Me 2: no MB part 2, acoustid names part 1, the preview matches part 2", func(t *testing.T) {
		identity := download.JudgeIdentity{Version: "v1", ISRC: "JPPO02000002", Title: "Nun To Me 2", Artist: "KANKAN", CatalogLengths: map[string]float64{"deezer": 188}}
		audio := download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-part1", "Nun To Me", 188, "KANKAN")}, Preview: preview(0.08), Length: 188}

		official := download.Judge(download.JudgeInput{Identity: identity, Audio: audio, Source: art, CandidateTitle: "Nun To Me 2"}, productionCuts)
		if official.Kind != download.JudgeProven || official.Grade != download.GradeA {
			t.Errorf("official upload = %s/%s, want proven/A; reasons: %v", official.Kind, official.Grade, official.Reasons)
		}
		reupload := download.Judge(download.JudgeInput{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "Nun To Me 2"}, productionCuts)
		if reupload.Kind != download.JudgeLikely {
			t.Errorf("fan re-upload = %s, want likely; reasons: %v", reupload.Kind, reupload.Reasons)
		}
	})

	t.Run("Autumn: a stale rejection expires when the identity is re-pinned", func(t *testing.T) {
		wrongPin := download.JudgeIdentity{Version: "pin-1", ISRC: "USAT20000001", Title: "Still The Same!", Artist: "Autumn!", CatalogLengths: map[string]float64{"deezer": 201}}
		audio := download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-part1", "Still The Same!", 201, "Autumn!")}, Preview: preview(0.40), Length: 201}
		rejected := download.Judge(download.JudgeInput{Identity: wrongPin, Audio: audio, Source: art, CandidateTitle: "Still The Same! 2"}, productionCuts)
		if rejected.Kind != download.JudgeContradicted {
			t.Fatalf("against the wrong pin = %s, want contradicted; reasons: %v", rejected.Kind, rejected.Reasons)
		}
		if !rejected.HoldsFor("pin-1") || rejected.HoldsFor("pin-2") {
			t.Error("a verdict holds only for the identity version it was judged against")
		}

		repinned := wrongPin
		repinned.Version, repinned.Title = "pin-2", "Still The Same! 2"
		audio.Preview = preview(0.06)
		rejudged := download.Judge(download.JudgeInput{Identity: repinned, Audio: audio, Source: art, CandidateTitle: "Still The Same! 2"}, productionCuts)
		if rejudged.Kind != download.JudgeProven || rejudged.JudgedAgainst != "pin-2" {
			t.Errorf("after the re-pin = %s against %q, want proven against pin-2; reasons: %v", rejudged.Kind, rejudged.JudgedAgainst, rejudged.Reasons)
		}
	})

	t.Run("Platinum Blonde Live: catalog lengths disagree and the preview matches", func(t *testing.T) {
		identity := download.JudgeIdentity{
			Version: "v1", ISRC: "ITA000000001", Title: "Live at City Hall", Artist: "Platinum Blonde",
			CatalogLengths: map[string]float64{"deezer": 251, "musicbrainz": 297},
		}
		audio := download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-studio", "City Hall", 297, "Platinum Blonde")}, Preview: preview(0.06), Length: 297}
		for source, want := range map[download.JudgeSource]download.JudgeKind{art: download.JudgeProven, fan: download.JudgeLikely} {
			got := download.Judge(download.JudgeInput{Identity: identity, Audio: audio, Source: source, CandidateTitle: "Live at City Hall"}, productionCuts)
			if got.Kind != want {
				t.Errorf("%s source = %s, want %s; reasons: %v", source.Kind, got.Kind, want, got.Reasons)
			}
		}
		withoutPreview := download.Judge(download.JudgeInput{Identity: identity, Audio: download.JudgeAudio{Length: 297}, Source: art, CandidateTitle: "Live at City Hall"}, productionCuts)
		if withoutPreview.Kind != download.JudgeUnknown || withoutPreview.Grade != download.GradeC {
			t.Errorf("no preview = %s/%s, want unknown/C", withoutPreview.Kind, withoutPreview.Grade)
		}
	})

	t.Run("Pavarotti: acoustid names another performance and the preview is absent", func(t *testing.T) {
		identity := download.JudgeIdentity{Version: "v1", ISRC: "ITB000000001", MBID: "mb-studio", Title: "Nessun Dorma", Artist: "Luciano Pavarotti", CatalogLengths: map[string]float64{"deezer": 195}}
		audio := download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-live", "Nessun Dorma (Live)", 195, "Luciano Pavarotti")}, Preview: preview(0.45), Length: 195}
		got := download.Judge(download.JudgeInput{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "Nessun Dorma"}, productionCuts)
		if got.Kind != download.JudgeContradicted || got.Grade != download.GradeF {
			t.Errorf("verdict = %s/%s, want contradicted/F; reasons: %v", got.Kind, got.Grade, got.Reasons)
		}
	})

	t.Run("Travis Scott outside: another song with the same title, AcoustID and length alone", func(t *testing.T) {
		identity := download.JudgeIdentity{Version: "v1", ISRC: "USSM10000001", MBID: "mb-ts", Title: "outside", Artist: "Travis Scott", CatalogLengths: map[string]float64{"deezer": 241}}
		audio := download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-other", "Outside", 215, "Another Artist")}, Length: 215}
		got := download.Judge(download.JudgeInput{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "outside"}, productionCuts)
		if got.Kind != download.JudgeContradicted {
			t.Errorf("verdict = %s, want contradicted; reasons: %v", got.Kind, got.Reasons)
		}
	})

	t.Run("unreleased track: no catalog identity is never contradicted, whatever the audio says", func(t *testing.T) {
		unreleased := download.JudgeIdentity{Version: "v1", Title: "Leaked Song", Artist: "Artist"}
		other := []download.AcoustIDResult{linked("mb-other", "Another Song", 200, "Someone")}
		answers := map[string]download.JudgeAudio{
			"an acoustid answer and a differing tag": {AcoustID: other, Length: 200},
			"a preview that is absent":               {Preview: preview(0.45), Length: 200},
			"both together":                          {AcoustID: other, Preview: preview(0.45), Length: 200},
			"nothing":                                {},
		}
		for name, audio := range answers {
			got := download.Judge(download.JudgeInput{Identity: unreleased, Audio: audio, Source: fan, CandidateTitle: "Leaked Song (Live)"}, productionCuts)
			if got.Kind != download.JudgeUnknown || got.Grade != download.GradeBestAvailable || len(got.Reasons) == 0 || got.JudgedAgainst != "v1" {
				t.Errorf("%s = %s/%s %v, want unknown/best_available with a reason", name, got.Kind, got.Grade, got.Reasons)
			}
		}
	})
}

func TestJudge_CallerFacts(t *testing.T) {
	art := download.JudgeSource{Kind: download.ArtTrack}
	identity := download.JudgeIdentity{
		Version: "v1", ISRC: "USRC10000001", MBID: "mb-bound", MergedMBIDs: []string{"mb-merged"}, Title: "Song", Artist: "Artist",
		CatalogLengths: map[string]float64{"deezer": 200},
	}
	noISRC := identity
	noISRC.ISRC = ""
	dup := []download.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}

	cases := []struct {
		name      string
		identity  download.JudgeIdentity
		audio     download.JudgeAudio
		wantKind  download.JudgeKind
		wantLower bool
	}{
		{"a merged MBID is the bound recording", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-merged", "Song", 200, "Artist")}, Length: 200}, download.JudgeProven, false},
		{"a link with no MBID names nothing, so it neither proves nor contradicts", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("", "Other Song", 200, "Someone")}, Preview: preview(0.40), Length: 200}, download.JudgeUnknown, false},
		{"links that carry no title name nothing", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-untitled", "", 200, "Someone")}, Preview: preview(0.40), Length: 200}, download.JudgeUnknown, false},
		{"a recording whose looked-up ISRCs include the bound one is the bound recording", identity, download.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", ISRCs: []string{"usrc10000001"}}}}, download.JudgeProven, false},
		{"a looked-up recording with another ISRC is another recording", identity, download.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", ISRCs: []string{"USRC19999999"}}}}, download.JudgeUnknown, false},
		{"a looked-up duplicate with no ISRC and the same version is likely", identity, download.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song"}}}, download.JudgeLikely, false},
		{"a looked-up duplicate whose disambiguation says live is another version", noISRC, download.JudgeAudio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", Disambiguation: "live"}}}, download.JudgeUnknown, false},
		{"a same-title link whose version cannot be looked up is not a duplicate when lookups are required", noISRC, download.JudgeAudio{AcoustID: dup, Length: 200, RequireVersionFacts: true}, download.JudgeUnknown, false},
		{"a same-title link is a duplicate by its own title when lookups are not required", noISRC, download.JudgeAudio{AcoustID: dup, Length: 200}, download.JudgeLikely, false},
		{"most surviving links naming other songs outvote one same-title duplicate", noISRC, download.JudgeAudio{AcoustID: []download.AcoustIDResult{
			linked("mb-dup", "Song", 200, "Artist"), linked("mb-a", "Other A", 200, "Someone"), linked("mb-b", "Other B", 200, "Someone"),
		}, Preview: preview(0.40), Length: 200}, download.JudgeContradicted, true},
		{"a bound link with a length outside every catalog length is only likely and lowers a stored grade", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: 262}, download.JudgeLikely, true},
		{"a bound link inside the catalog length is proven and lowers nothing", identity, download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: 201}, download.JudgeProven, false},
		{"a length outside every catalog length with no fingerprint answer lowers nothing", identity, download.JudgeAudio{Length: 262}, download.JudgeUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := download.Judge(download.JudgeInput{Identity: tc.identity, Audio: tc.audio, Source: art, CandidateTitle: "Song"}, productionCuts)
			if got.Kind != tc.wantKind || got.LowersStoredGrade != tc.wantLower {
				t.Errorf("verdict = %s lowers=%v, want %s lowers=%v; reasons: %v", got.Kind, got.LowersStoredGrade, tc.wantKind, tc.wantLower, got.Reasons)
			}
		})
	}
}

func TestJudge_ALengthContradictionLowersTheGradeBelowA(t *testing.T) {
	identity := download.JudgeIdentity{Version: "v1", MBID: "mb-bound", Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}
	audio := func(length float64) download.JudgeAudio {
		return download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: length}
	}

	fits := download.Judge(download.JudgeInput{Identity: identity, Audio: audio(200), Source: download.JudgeSource{Kind: download.ArtTrack}, CandidateTitle: "Song"}, productionCuts)
	wrongLength := download.Judge(download.JudgeInput{Identity: identity, Audio: audio(262), Source: download.JudgeSource{Kind: download.ArtTrack}, CandidateTitle: "Song"}, productionCuts)

	if fits.Grade != download.GradeA || wrongLength.Grade != download.GradeB || !wrongLength.Grade.IsKnown() {
		t.Errorf("grades = %s and %s, want A for the fitting file and B for the wrong-length one", fits.Grade, wrongLength.Grade)
	}
}

func TestJudge_ADoubtedReferenceCapsAFingerprintProofAtLikely(t *testing.T) {
	identity := download.JudgeIdentity{Version: "v1", MBID: "mb-bound", Doubted: true, Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}

	byAcoustID := download.Judge(download.JudgeInput{Identity: identity, Audio: download.JudgeAudio{AcoustID: []download.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, Source: download.JudgeSource{Kind: download.ArtTrack}, CandidateTitle: "Song"}, productionCuts)
	byPreview := download.Judge(download.JudgeInput{Identity: identity, Audio: download.JudgeAudio{Preview: preview(0.05), Length: 200}, Source: download.JudgeSource{Kind: download.ArtTrack}, CandidateTitle: "Song"}, productionCuts)

	if byAcoustID.Kind != download.JudgeLikely || byPreview.Kind != download.JudgeLikely || byAcoustID.LowersStoredGrade {
		t.Errorf("verdicts = %s and %s (lowers %v), want likely for both and no lowering", byAcoustID.Kind, byPreview.Kind, byAcoustID.LowersStoredGrade)
	}
}

func TestJudge_AUserUploadIsProvenByAPreviewAtTheCutWhateverItsSource(t *testing.T) {
	identity := download.JudgeIdentity{Version: "v1", ISRC: "USRC10000001", Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}

	got := download.Judge(download.JudgeInput{Identity: identity, Audio: download.JudgeAudio{Preview: preview(0.08), Length: 200}, Source: download.JudgeSource{Kind: download.UserUpload}}, productionCuts)

	if got.Kind != download.JudgeProven {
		t.Errorf("verdict = %s, want proven: a user's own file needs no official source", got.Kind)
	}
}

func TestCutsWithProvenMax_TheProvenCutComesFromTheCallerAndNeverExceedsTheLikelyCut(t *testing.T) {
	loose := download.JudgeCutsWithProvenMax(0.25)
	strict := download.JudgeCutsWithProvenMax(0.05)

	if loose.ProvenMax != 0.25 || loose.LikelyMax < loose.ProvenMax {
		t.Errorf("loose cuts = %+v, want a proven cut of 0.25 under the likely cut", loose)
	}
	if strict.ProvenMax != 0.05 || strict.LikelyMax != productionCuts.LikelyMax || strict.AbsentMin != productionCuts.AbsentMin {
		t.Errorf("strict cuts = %+v, want only the proven cut changed", strict)
	}
}
