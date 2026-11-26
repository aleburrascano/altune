package audioverdict_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/audioverdict"
	"altune/go-api/internal/download/internal/core/ports"
	"testing"
)

var productionCuts = audioverdict.CutsWithProvenMax(0.10)

func linked(mbid, title string, length float64, artists ...string) ports.AcoustIDResult {
	return ports.AcoustIDResult{ID: "aid-" + mbid, Score: 0.97, Recordings: []ports.LinkedRecording{{MBID: mbid, Title: title, Artists: artists, Duration: length}}}
}

func preview(rate float64) *audioverdict.Preview {
	return &audioverdict.Preview{BitErrorRate: rate, OverlapSeconds: 29}
}

func TestJudge_Table(t *testing.T) {
	identity := audioverdict.Identity{
		Version: "v1", ISRC: "USRC10000001", MBID: "mb-bound", Title: "Song", Artist: "Artist",
		CatalogLengths: map[string]float64{"deezer": 200, "musicbrainz": 201},
	}
	noISRC := identity
	noISRC.ISRC = ""
	art := audioverdict.Source{Kind: audioquality.ArtTrack}
	fan := audioverdict.Source{Kind: audioquality.FanUpload}

	cases := []struct {
		name      string
		identity  audioverdict.Identity
		audio     audioverdict.Audio
		source    audioverdict.Source
		candidate string
		kind      audioverdict.Kind
		grade     audioquality.Grade
	}{
		{"acoustid links the bound recording with matching length", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, art, "Song", audioverdict.Proven, audioquality.A},
		{"acoustid links the bound recording when the audio's length is unknown", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 0}, art, "Song", audioverdict.Proven, audioquality.A},
		{"acoustid proof on a fan upload grades B", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, fan, "Song", audioverdict.Proven, audioquality.B},
		{"acoustid links the bound recording at a length 100s off is no answer", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 300, "Artist")}, Length: 200}, art, "Song", audioverdict.Unknown, audioquality.C},
		{"preview at the A cut on an official source with matching tags", identity, audioverdict.Audio{Preview: preview(0.10), Length: 200}, art, "Song", audioverdict.Proven, audioquality.A},
		{"preview at the A cut on a fan upload is only likely", identity, audioverdict.Audio{Preview: preview(0.10), Length: 200}, fan, "Song", audioverdict.Likely, audioquality.B},
		{"preview at the A cut but the version tags differ is only likely", identity, audioverdict.Audio{Preview: preview(0.05), Length: 200}, art, "Song (Live)", audioverdict.Likely, audioquality.B},
		{"preview at 0.18 is likely", identity, audioverdict.Audio{Preview: preview(0.18), Length: 200}, art, "Song", audioverdict.Likely, audioquality.B},
		{"preview at 0.19 proves nothing", identity, audioverdict.Audio{Preview: preview(0.19), Length: 200}, art, "Song", audioverdict.Unknown, audioquality.C},
		{"preview overlap too short is no preview", identity, audioverdict.Audio{Preview: &audioverdict.Preview{BitErrorRate: 0.02, OverlapSeconds: 5}, Length: 200}, art, "Song", audioverdict.Unknown, audioquality.C},
		{"same-title acoustid duplicate with no ISRC is likely", noISRC, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Length: 200}, art, "Song", audioverdict.Likely, audioquality.B},
		{"same-title acoustid duplicate with an ISRC is another recording, one source, unknown", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Length: 200}, art, "Song", audioverdict.Unknown, audioquality.D},
		{"same-title duplicate but the preview is absent is not likely", noISRC, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}, Preview: preview(0.40), Length: 200}, art, "Song", audioverdict.Unknown, audioquality.C},
		{"no catalog identity is unknown and best available, whatever the audio says", audioverdict.Identity{Version: "v1", Title: "Song", Artist: "Artist"}, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.45), Length: 200}, fan, "Song (Live)", audioverdict.Unknown, audioquality.BestAvailable},
		{"nothing to go on is unknown", identity, audioverdict.Audio{}, art, "Song", audioverdict.Unknown, audioquality.D},
		{"a part number alone never contradicts", identity, audioverdict.Audio{Length: 200}, art, "Song pt 2", audioverdict.Unknown, audioquality.C},
		{"a preview that is absent alone never contradicts", identity, audioverdict.Audio{Preview: preview(0.45), Length: 200}, art, "Song", audioverdict.Unknown, audioquality.C},
		{"another acoustid recording alone never contradicts", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Length: 200}, art, "Song", audioverdict.Unknown, audioquality.D},
		{"another acoustid recording and an absent preview contradict", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.40), Length: 200}, art, "Song", audioverdict.Contradicted, audioquality.F},
		{"a different version tag and a length outside the window contradict", identity, audioverdict.Audio{Length: 260}, art, "Song (Live)", audioverdict.Contradicted, audioquality.F},
		{"a length outside the window alone never contradicts", identity, audioverdict.Audio{Length: 260}, art, "Song", audioverdict.Unknown, audioquality.D},
		{"a matching preview vetoes two sources against", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-other", "Other Song", 200, "Someone")}, Preview: preview(0.12), Length: 200}, fan, "Song", audioverdict.Likely, audioquality.B},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := audioverdict.Judge(audioverdict.Input{Identity: tc.identity, Audio: tc.audio, Source: tc.source, CandidateTitle: tc.candidate}, productionCuts)
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
	art := audioverdict.Source{Kind: audioquality.ArtTrack}
	fan := audioverdict.Source{Kind: audioquality.FanUpload}

	t.Run("KANKAN Nun To Me 2: no MB part 2, acoustid names part 1, the preview matches part 2", func(t *testing.T) {
		identity := audioverdict.Identity{Version: "v1", ISRC: "JPPO02000002", Title: "Nun To Me 2", Artist: "KANKAN", CatalogLengths: map[string]float64{"deezer": 188}}
		audio := audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-part1", "Nun To Me", 188, "KANKAN")}, Preview: preview(0.08), Length: 188}

		official := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio, Source: art, CandidateTitle: "Nun To Me 2"}, productionCuts)
		if official.Kind != audioverdict.Proven || official.Grade != audioquality.A {
			t.Errorf("official upload = %s/%s, want proven/A; reasons: %v", official.Kind, official.Grade, official.Reasons)
		}
		reupload := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "Nun To Me 2"}, productionCuts)
		if reupload.Kind != audioverdict.Likely {
			t.Errorf("fan re-upload = %s, want likely; reasons: %v", reupload.Kind, reupload.Reasons)
		}
	})

	t.Run("Autumn: a stale rejection expires when the identity is re-pinned", func(t *testing.T) {
		wrongPin := audioverdict.Identity{Version: "pin-1", ISRC: "USAT20000001", Title: "Still The Same!", Artist: "Autumn!", CatalogLengths: map[string]float64{"deezer": 201}}
		audio := audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-part1", "Still The Same!", 201, "Autumn!")}, Preview: preview(0.40), Length: 201}
		rejected := audioverdict.Judge(audioverdict.Input{Identity: wrongPin, Audio: audio, Source: art, CandidateTitle: "Still The Same! 2"}, productionCuts)
		if rejected.Kind != audioverdict.Contradicted {
			t.Fatalf("against the wrong pin = %s, want contradicted; reasons: %v", rejected.Kind, rejected.Reasons)
		}
		if !rejected.HoldsFor("pin-1") || rejected.HoldsFor("pin-2") {
			t.Error("a verdict holds only for the identity version it was judged against")
		}

		repinned := wrongPin
		repinned.Version, repinned.Title = "pin-2", "Still The Same! 2"
		audio.Preview = preview(0.06)
		rejudged := audioverdict.Judge(audioverdict.Input{Identity: repinned, Audio: audio, Source: art, CandidateTitle: "Still The Same! 2"}, productionCuts)
		if rejudged.Kind != audioverdict.Proven || rejudged.JudgedAgainst != "pin-2" {
			t.Errorf("after the re-pin = %s against %q, want proven against pin-2; reasons: %v", rejudged.Kind, rejudged.JudgedAgainst, rejudged.Reasons)
		}
	})

	t.Run("Platinum Blonde Live: catalog lengths disagree and the preview matches", func(t *testing.T) {
		identity := audioverdict.Identity{
			Version: "v1", ISRC: "ITA000000001", Title: "Live at City Hall", Artist: "Platinum Blonde",
			CatalogLengths: map[string]float64{"deezer": 251, "musicbrainz": 297},
		}
		audio := audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-studio", "City Hall", 297, "Platinum Blonde")}, Preview: preview(0.06), Length: 297}
		for source, want := range map[audioverdict.Source]audioverdict.Kind{art: audioverdict.Proven, fan: audioverdict.Likely} {
			got := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio, Source: source, CandidateTitle: "Live at City Hall"}, productionCuts)
			if got.Kind != want {
				t.Errorf("%s source = %s, want %s; reasons: %v", source.Kind, got.Kind, want, got.Reasons)
			}
		}
		withoutPreview := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audioverdict.Audio{Length: 297}, Source: art, CandidateTitle: "Live at City Hall"}, productionCuts)
		if withoutPreview.Kind != audioverdict.Unknown || withoutPreview.Grade != audioquality.C {
			t.Errorf("no preview = %s/%s, want unknown/C", withoutPreview.Kind, withoutPreview.Grade)
		}
	})

	t.Run("Pavarotti: acoustid names another performance and the preview is absent", func(t *testing.T) {
		identity := audioverdict.Identity{Version: "v1", ISRC: "ITB000000001", MBID: "mb-studio", Title: "Nessun Dorma", Artist: "Luciano Pavarotti", CatalogLengths: map[string]float64{"deezer": 195}}
		audio := audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-live", "Nessun Dorma (Live)", 195, "Luciano Pavarotti")}, Preview: preview(0.45), Length: 195}
		got := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "Nessun Dorma"}, productionCuts)
		if got.Kind != audioverdict.Contradicted || got.Grade != audioquality.F {
			t.Errorf("verdict = %s/%s, want contradicted/F; reasons: %v", got.Kind, got.Grade, got.Reasons)
		}
	})

	t.Run("Travis Scott outside: another song with the same title, AcoustID and length alone", func(t *testing.T) {
		identity := audioverdict.Identity{Version: "v1", ISRC: "USSM10000001", MBID: "mb-ts", Title: "outside", Artist: "Travis Scott", CatalogLengths: map[string]float64{"deezer": 241}}
		audio := audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-other", "Outside", 215, "Another Artist")}, Length: 215}
		got := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio, Source: fan, CandidateTitle: "outside"}, productionCuts)
		if got.Kind != audioverdict.Contradicted {
			t.Errorf("verdict = %s, want contradicted; reasons: %v", got.Kind, got.Reasons)
		}
	})

	t.Run("unreleased track: no catalog identity is never contradicted, whatever the audio says", func(t *testing.T) {
		unreleased := audioverdict.Identity{Version: "v1", Title: "Leaked Song", Artist: "Artist"}
		other := []ports.AcoustIDResult{linked("mb-other", "Another Song", 200, "Someone")}
		answers := map[string]audioverdict.Audio{
			"an acoustid answer and a differing tag": {AcoustID: other, Length: 200},
			"a preview that is absent":               {Preview: preview(0.45), Length: 200},
			"both together":                          {AcoustID: other, Preview: preview(0.45), Length: 200},
			"nothing":                                {},
		}
		for name, audio := range answers {
			got := audioverdict.Judge(audioverdict.Input{Identity: unreleased, Audio: audio, Source: fan, CandidateTitle: "Leaked Song (Live)"}, productionCuts)
			if got.Kind != audioverdict.Unknown || got.Grade != audioquality.BestAvailable || len(got.Reasons) == 0 || got.JudgedAgainst != "v1" {
				t.Errorf("%s = %s/%s %v, want unknown/best_available with a reason", name, got.Kind, got.Grade, got.Reasons)
			}
		}
	})
}

func TestJudge_CallerFacts(t *testing.T) {
	art := audioverdict.Source{Kind: audioquality.ArtTrack}
	identity := audioverdict.Identity{
		Version: "v1", ISRC: "USRC10000001", MBID: "mb-bound", MergedMBIDs: []string{"mb-merged"}, Title: "Song", Artist: "Artist",
		CatalogLengths: map[string]float64{"deezer": 200},
	}
	noISRC := identity
	noISRC.ISRC = ""
	dup := []ports.AcoustIDResult{linked("mb-dup", "Song", 200, "Artist")}

	cases := []struct {
		name      string
		identity  audioverdict.Identity
		audio     audioverdict.Audio
		wantKind  audioverdict.Kind
		wantLower bool
	}{
		{"a merged MBID is the bound recording", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-merged", "Song", 200, "Artist")}, Length: 200}, audioverdict.Proven, false},
		{"a link with no MBID names nothing, so it neither proves nor contradicts", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("", "Other Song", 200, "Someone")}, Preview: preview(0.40), Length: 200}, audioverdict.Unknown, false},
		{"links that carry no title name nothing", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-untitled", "", 200, "Someone")}, Preview: preview(0.40), Length: 200}, audioverdict.Unknown, false},
		{"a recording whose looked-up ISRCs include the bound one is the bound recording", identity, audioverdict.Audio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", ISRCs: []string{"usrc10000001"}}}}, audioverdict.Proven, false},
		{"a looked-up recording with another ISRC is another recording", identity, audioverdict.Audio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", ISRCs: []string{"USRC19999999"}}}}, audioverdict.Unknown, false},
		{"a looked-up duplicate with no ISRC and the same version is likely", identity, audioverdict.Audio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song"}}}, audioverdict.Likely, false},
		{"a looked-up duplicate whose disambiguation says live is another version", noISRC, audioverdict.Audio{AcoustID: dup, Length: 200, RecordingFacts: map[string]download.RecordingFacts{"mb-dup": {MBID: "mb-dup", Title: "Song", Disambiguation: "live"}}}, audioverdict.Unknown, false},
		{"a same-title link whose version cannot be looked up is not a duplicate when lookups are required", noISRC, audioverdict.Audio{AcoustID: dup, Length: 200, RequireVersionFacts: true}, audioverdict.Unknown, false},
		{"a same-title link is a duplicate by its own title when lookups are not required", noISRC, audioverdict.Audio{AcoustID: dup, Length: 200}, audioverdict.Likely, false},
		{"most surviving links naming other songs outvote one same-title duplicate", noISRC, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{
			linked("mb-dup", "Song", 200, "Artist"), linked("mb-a", "Other A", 200, "Someone"), linked("mb-b", "Other B", 200, "Someone"),
		}, Preview: preview(0.40), Length: 200}, audioverdict.Contradicted, true},
		{"a bound link with a length outside every catalog length is only likely and lowers a stored grade", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: 262}, audioverdict.Likely, true},
		{"a bound link inside the catalog length is proven and lowers nothing", identity, audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: 201}, audioverdict.Proven, false},
		{"a length outside every catalog length with no fingerprint answer lowers nothing", identity, audioverdict.Audio{Length: 262}, audioverdict.Unknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := audioverdict.Judge(audioverdict.Input{Identity: tc.identity, Audio: tc.audio, Source: art, CandidateTitle: "Song"}, productionCuts)
			if got.Kind != tc.wantKind || got.LowersStoredGrade != tc.wantLower {
				t.Errorf("verdict = %s lowers=%v, want %s lowers=%v; reasons: %v", got.Kind, got.LowersStoredGrade, tc.wantKind, tc.wantLower, got.Reasons)
			}
		})
	}
}

func TestJudge_ALengthContradictionLowersTheGradeBelowA(t *testing.T) {
	identity := audioverdict.Identity{Version: "v1", MBID: "mb-bound", Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}
	audio := func(length float64) audioverdict.Audio {
		return audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 0, "Artist")}, Length: length}
	}

	fits := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio(200), Source: audioverdict.Source{Kind: audioquality.ArtTrack}, CandidateTitle: "Song"}, productionCuts)
	wrongLength := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audio(262), Source: audioverdict.Source{Kind: audioquality.ArtTrack}, CandidateTitle: "Song"}, productionCuts)

	if fits.Grade != audioquality.A || wrongLength.Grade != audioquality.B || !wrongLength.Grade.IsKnown() {
		t.Errorf("grades = %s and %s, want A for the fitting file and B for the wrong-length one", fits.Grade, wrongLength.Grade)
	}
}

func TestJudge_ADoubtedReferenceCapsAFingerprintProofAtLikely(t *testing.T) {
	identity := audioverdict.Identity{Version: "v1", MBID: "mb-bound", Doubted: true, Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}

	byAcoustID := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audioverdict.Audio{AcoustID: []ports.AcoustIDResult{linked("mb-bound", "Song", 200, "Artist")}, Length: 200}, Source: audioverdict.Source{Kind: audioquality.ArtTrack}, CandidateTitle: "Song"}, productionCuts)
	byPreview := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audioverdict.Audio{Preview: preview(0.05), Length: 200}, Source: audioverdict.Source{Kind: audioquality.ArtTrack}, CandidateTitle: "Song"}, productionCuts)

	if byAcoustID.Kind != audioverdict.Likely || byPreview.Kind != audioverdict.Likely || byAcoustID.LowersStoredGrade {
		t.Errorf("verdicts = %s and %s (lowers %v), want likely for both and no lowering", byAcoustID.Kind, byPreview.Kind, byAcoustID.LowersStoredGrade)
	}
}

func TestJudge_AUserUploadIsProvenByAPreviewAtTheCutWhateverItsSource(t *testing.T) {
	identity := audioverdict.Identity{Version: "v1", ISRC: "USRC10000001", Title: "Song", Artist: "Artist", CatalogLengths: map[string]float64{"deezer": 200}}

	got := audioverdict.Judge(audioverdict.Input{Identity: identity, Audio: audioverdict.Audio{Preview: preview(0.08), Length: 200}, Source: audioverdict.Source{Kind: audioquality.UserUpload}}, productionCuts)

	if got.Kind != audioverdict.Proven {
		t.Errorf("verdict = %s, want proven: a user's own file needs no official source", got.Kind)
	}
}

func TestCutsWithProvenMax_TheProvenCutComesFromTheCallerAndNeverExceedsTheLikelyCut(t *testing.T) {
	loose := audioverdict.CutsWithProvenMax(0.25)
	strict := audioverdict.CutsWithProvenMax(0.05)

	if loose.ProvenMax != 0.25 || loose.LikelyMax < loose.ProvenMax {
		t.Errorf("loose cuts = %+v, want a proven cut of 0.25 under the likely cut", loose)
	}
	if strict.ProvenMax != 0.05 || strict.LikelyMax != productionCuts.LikelyMax || strict.AbsentMin != productionCuts.AbsentMin {
		t.Errorf("strict cuts = %+v, want only the proven cut changed", strict)
	}
}
