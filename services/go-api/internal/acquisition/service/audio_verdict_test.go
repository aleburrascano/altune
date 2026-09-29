package service

import (
	"altune/go-api/internal/acquisition/ports"
	"math"
	"slices"
	"testing"
)

func linked(mbid, title string, duration float64, artists ...string) ports.LinkedRecording {
	return ports.LinkedRecording{MBID: mbid, Title: title, Artists: artists, Duration: duration}
}

func acoustIDResult(id string, score float64, recordings ...ports.LinkedRecording) ports.AcoustIDResult {
	return ports.AcoustIDResult{ID: id, Score: score, Recordings: recordings}
}

func survivingMBIDs(verdict AudioVerdict) []string {
	mbids := make([]string, 0, len(verdict.Surviving))
	for _, recording := range verdict.Surviving {
		mbids = append(mbids, recording.MBID)
	}
	return mbids
}

var drinkingInLA = AudioReference{Title: "Drinking in L.A.", Artist: "Bran Van 3000", Duration: 236, MBIDs: []string{"5d6efd30"}}

var drinkingInLAResult = acoustIDResult("5d5d307d", 0.93,
	linked("5d6efd30", "Drinking in L.A.", 236, "Bran Van 3000"),
	linked("edit-mbid", "Drinking in L.A. (edit)", 220, "Bran Van 3000"),
	linked("thinking-mbid", "Thinking in L.A.", 342, "Bran Van 3000"),
)

type verdictCase struct {
	name          string
	ref           AudioReference
	audioDuration float64
	results       []ports.AcoustIDResult
	want          VerdictKind
}

func assertVerdicts(t *testing.T, cases []verdictCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyAudio(tc.ref, tc.audioDuration, tc.results)

			if got.Kind != tc.want {
				t.Errorf("ClassifyAudio(%+v, %v, %+v).Kind = %q, want %q", tc.ref, tc.audioDuration, tc.results, got.Kind, tc.want)
			}
		})
	}
}

func TestClassifyAudio_StudyCases(t *testing.T) {
	assertVerdicts(t, []verdictCase{
		{
			name: "Drinking in L.A. links its own recording beside an edit and a different song, so the audio is hard",
			ref:  drinkingInLA, audioDuration: 236,
			results: []ports.AcoustIDResult{drinkingInLAResult},
			want:    VerdictHard,
		},
		{
			name:          "Cutting Crew (I Just) Died in Your Arms matches a sibling recording by title artist and length, so the audio is soft",
			ref:           AudioReference{Title: "(I Just) Died in Your Arms", Artist: "Cutting Crew", Duration: 280, MBIDs: []string{"canonical"}},
			audioDuration: 281,
			results: []ports.AcoustIDResult{acoustIDResult("cc", 0.88,
				linked("sibling", "(I Just) Died in Your Arms", 279, "Cutting Crew"))},
			want: VerdictSoft,
		},
		{
			name:          "Pavarotti Nessun dorma only links live recordings, so the audio is another version",
			ref:           AudioReference{Title: "Nessun dorma", Artist: "Luciano Pavarotti", Duration: 180, MBIDs: []string{"studio"}},
			audioDuration: 181,
			results: []ports.AcoustIDResult{acoustIDResult("nd", 0.9,
				linked("live-1", "Nessun dorma (live)", 182, "Luciano Pavarotti"),
				linked("live-2", "Nessun dorma (Live at the Baths of Caracalla)", 179, "Luciano Pavarotti"))},
			want: VerdictOtherVersion,
		},
		{
			name:          "Bee Gees Night Fever medley for a Night Fever track is a different song",
			ref:           AudioReference{Title: "Night Fever", Artist: "Bee Gees", Duration: 213, MBIDs: []string{"night-fever"}},
			audioDuration: 214,
			results: []ports.AcoustIDResult{acoustIDResult("bg", 0.8,
				linked("medley", "Night Fever / More Than a Woman", 215, "Bee Gees"))},
			want: VerdictDifferentSong,
		},
		{
			name: "no AcoustID results leaves the audio unknown",
			ref:  drinkingInLA, audioDuration: 236,
			results: nil,
			want:    VerdictUnknown,
		},
	})
}

func TestClassifyAudio_LengthFilterKeepsOnlyLinksNearTheAudioLength(t *testing.T) {
	verdict := ClassifyAudio(drinkingInLA, 236, []ports.AcoustIDResult{drinkingInLAResult})

	if got, want := survivingMBIDs(verdict), []string{"5d6efd30"}; !slices.Equal(got, want) {
		t.Errorf("surviving links for 236s audio = %v, want %v", got, want)
	}
}

func TestClassifyAudio_LengthFilterBoundaries(t *testing.T) {
	tests := []struct {
		name          string
		audioDuration float64
		link          float64
		wantSurvives  bool
	}{
		{name: "a zero-duration link has unknown length and survives", audioDuration: 236, link: 0, wantSurvives: true},
		{name: "just inside three percent of long audio survives", audioDuration: 236, link: 243, wantSurvives: true},
		{name: "just past three percent of long audio is dropped", audioDuration: 236, link: 243.2, wantSurvives: false},
		{name: "a short link below the window is dropped", audioDuration: 236, link: 228.8, wantSurvives: false},
		{name: "five seconds is the floor for short audio", audioDuration: 100, link: 105, wantSurvives: true},
		{name: "just past five seconds on short audio is dropped", audioDuration: 100, link: 105.1, wantSurvives: false},
		{name: "audio of unknown length drops no link", audioDuration: 0, link: 342, wantSurvives: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := []ports.AcoustIDResult{acoustIDResult("a", 0.9, linked("link", "Drinking in L.A.", tt.link, "Bran Van 3000"))}

			verdict := ClassifyAudio(drinkingInLA, tt.audioDuration, results)

			if survives := len(verdict.Surviving) == 1; survives != tt.wantSurvives {
				t.Errorf("link of %vs for %vs audio survives = %v, want %v", tt.link, tt.audioDuration, survives, tt.wantSurvives)
			}
		})
	}
}

var dontStopMeNow = AudioReference{Title: "Don't Stop Me Now", Artist: "Queen", Duration: 209, MBIDs: []string{"canonical"}}

func singleLink(recording ports.LinkedRecording) []ports.AcoustIDResult {
	return []ports.AcoustIDResult{acoustIDResult("q", 0.85, recording)}
}

func TestClassifyAudio_SoftNeedsTitleQualifierArtistAndLengthToAgree(t *testing.T) {
	longerRef := dontStopMeNow
	longerRef.Duration = 240
	assertVerdicts(t, []verdictCase{
		{
			name: "title qualifier artist and length all agree", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("sibling", "Don’t Stop Me Now", 210, "Queen")),
			want:    VerdictSoft,
		},
		{
			name: "a different core title is a different song", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("sibling", "Don't Stop Believin'", 210, "Queen")),
			want:    VerdictDifferentSong,
		},
		{
			name: "an unrequested qualifier is another version", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("sibling", "Don't Stop Me Now (remix)", 210, "Queen")),
			want:    VerdictOtherVersion,
		},
		{
			name: "an instrumental title is another version", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("sibling", "Don't Stop Me Now (Instrumental)", 210, "Queen")),
			want:    VerdictOtherVersion,
		},
		{
			name: "no overlapping artist is a different song", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("sibling", "Don't Stop Me Now", 210, "Journey")),
			want:    VerdictDifferentSong,
		},
		{
			name: "a reference length outside the window is a different song", ref: longerRef, audioDuration: 210,
			results: singleLink(linked("sibling", "Don't Stop Me Now", 0, "Queen")),
			want:    VerdictDifferentSong,
		},
	})
}

func TestClassifyAudio_SoftAgreementDetails(t *testing.T) {
	assertVerdicts(t, []verdictCase{
		{
			name: "a left curly apostrophe matches a straight one",
			ref:  AudioReference{Title: "Rock ‘n’ Roll", Artist: "Queen"}, audioDuration: 200,
			results: singleLink(linked("s", "Rock 'n' Roll", 200, "Queen")),
			want:    VerdictSoft,
		},
		{
			name: "a featured artist on the reference overlaps a recording credit",
			ref:  AudioReference{Title: "Under Pressure", Artist: "Queen feat. David Bowie"}, audioDuration: 248,
			results: singleLink(linked("s", "Under Pressure", 248, "David Bowie")),
			want:    VerdictSoft,
		},
		{
			name: "a joint credit split on ampersand overlaps the reference",
			ref:  AudioReference{Title: "Under Pressure", Artist: "Queen"}, audioDuration: 248,
			results: singleLink(linked("s", "Under Pressure", 248, "Queen & David Bowie")),
			want:    VerdictSoft,
		},
		{
			name: "a qualifier the reference asks for is not unrequested",
			ref:  AudioReference{Title: "Nessun dorma (live)", Artist: "Luciano Pavarotti", Duration: 180}, audioDuration: 181,
			results: singleLink(linked("s", "Nessun Dorma (Live)", 182, "Luciano Pavarotti")),
			want:    VerdictSoft,
		},
		{
			name: "a reference length cannot agree with audio of unknown length",
			ref:  dontStopMeNow, audioDuration: 0,
			results: singleLink(linked("s", "Don't Stop Me Now", 210, "Queen")),
			want:    VerdictDifferentSong,
		},
		{
			name: "a qualifier in fullwidth brackets is still an unrequested qualifier",
			ref:  dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "Don't Stop Me Now（Live）", 210, "Queen")),
			want:    VerdictOtherVersion,
		},
		{
			name: "a reference shorter than the five-second floor cannot agree with audio of unknown length",
			ref:  AudioReference{Title: "Intro", Artist: "Queen", Duration: 3}, audioDuration: 0,
			results: singleLink(linked("s", "Intro", 3, "Queen")),
			want:    VerdictDifferentSong,
		},
		{
			name: "an empty reference title never agrees with an empty recording title",
			ref:  AudioReference{Artist: "Queen"}, audioDuration: 200,
			results: singleLink(linked("s", "", 200, "Queen")),
			want:    VerdictDifferentSong,
		},
	})
}

func TestClassifyAudio_MajorityOfOtherTitlesOverridesSoftButNotHard(t *testing.T) {
	sibling := linked("sibling", "Don't Stop Me Now", 210, "Queen")
	other := linked("other", "Bohemian Rhapsody", 210, "Queen")
	another := linked("another", "Somebody to Love", 210, "Queen")
	untitled := linked("untitled", "", 210, "Queen")
	assertVerdicts(t, []verdictCase{
		{
			name: "two other titles out of three downgrade soft to a different song", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, sibling, other, another)},
			want:    VerdictDifferentSong,
		},
		{
			name: "one other title out of two is not a majority", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, sibling, other, other)},
			want:    VerdictSoft,
		},
		{
			name: "an untitled link is not counted as another title", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, sibling, other, untitled)},
			want:    VerdictSoft,
		},
		{
			name: "a hard match is never downgraded", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, linked("canonical", "Don't Stop Me Now", 210, "Queen"), other, another)},
			want:    VerdictHard,
		},
	})
}

func TestClassifyAudio_ResultsWithoutUsableLinks(t *testing.T) {
	blankMBIDs := dontStopMeNow
	blankMBIDs.MBIDs = []string{""}
	assertVerdicts(t, []verdictCase{
		{
			name: "results whose links all fail the length filter are a different song", ref: drinkingInLA, audioDuration: 300,
			results: []ports.AcoustIDResult{drinkingInLAResult},
			want:    VerdictDifferentSong,
		},
		{
			name: "a result with no linked recordings is a different song", ref: drinkingInLA, audioDuration: 236,
			results: []ports.AcoustIDResult{acoustIDResult("bare", 0.7)},
			want:    VerdictDifferentSong,
		},
		{
			name: "a blank MBID never makes a hard match", ref: blankMBIDs, audioDuration: 210,
			results: singleLink(linked("", "Bohemian Rhapsody", 210, "Queen")),
			want:    VerdictDifferentSong,
		},
		{
			name: "audio of unknown length still matches its own recording", ref: drinkingInLA, audioDuration: 0,
			results: []ports.AcoustIDResult{drinkingInLAResult},
			want:    VerdictHard,
		},
	})
}

func TestClassifyAudio_ScoreIsTheTopResultScore(t *testing.T) {
	results := []ports.AcoustIDResult{acoustIDResult("low", 0.6), drinkingInLAResult, acoustIDResult("mid", 0.7)}

	verdict := ClassifyAudio(drinkingInLA, 236, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93", verdict.Score)
	}
}

func TestClassifyAudio_UnknownCarriesNoScoreOrLinks(t *testing.T) {
	verdict := ClassifyAudio(drinkingInLA, 236, nil)

	if verdict.Score != 0 || verdict.Surviving != nil {
		t.Errorf("unknown verdict = %+v, want zero score and no links", verdict)
	}
}

func TestClassifyAudio_ScoreIgnoresANaNResultScore(t *testing.T) {
	results := []ports.AcoustIDResult{
		acoustIDResult("malformed", math.NaN()),
		drinkingInLAResult,
		acoustIDResult("mid", 0.7),
	}

	verdict := ClassifyAudio(drinkingInLA, 236, results)

	if verdict.Score != 0.93 {
		t.Errorf("Score = %v, want 0.93 (a NaN score must not poison the top score)", verdict.Score)
	}
}

func TestClassifyAudio_NegativeReferenceLengthDisagrees(t *testing.T) {
	ref := dontStopMeNow
	ref.Duration = -5

	verdict := ClassifyAudio(ref, 210, singleLink(linked("sibling", "Don't Stop Me Now", 210, "Queen")))

	if verdict.Kind != VerdictDifferentSong {
		t.Errorf("Kind = %q, want %q (a negative reference duration is not unknown, it disagrees)", verdict.Kind, VerdictDifferentSong)
	}
}

func TestClassifyAudio_ProbeMajorityRule(t *testing.T) {
	sibling := linked("sibling", "Don't Stop Me Now", 210, "Queen")
	live := linked("live", "Don't Stop Me Now (live)", 210, "Queen")
	longOther := linked("long-other", "Bohemian Rhapsody", 355, "Queen")
	longAnother := linked("long-another", "Somebody to Love", 296, "Queen")
	other := linked("other", "Bohemian Rhapsody", 210, "Queen")
	another := linked("another", "Somebody to Love", 210, "Queen")
	assertVerdicts(t, []verdictCase{
		{
			name: "other titles dropped by the length filter do not outvote a soft match", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, sibling, longOther, longAnother)},
			want:    VerdictSoft,
		},
		{
			name: "versions of the same core title do not count as other titles", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, sibling,
				linked("remix", "Don't Stop Me Now (remix)", 210, "Queen"),
				linked("live", "Don't Stop Me Now (live)", 210, "Queen"))},
			want: VerdictSoft,
		},
		{
			name: "a majority of other titles leaves another version as another version", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9, live, other, another)},
			want:    VerdictOtherVersion,
		},
		{
			name: "other titles spread across several results still downgrade soft", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{
				acoustIDResult("a", 0.9, sibling),
				acoustIDResult("b", 0.8, other),
				acoustIDResult("c", 0.7, another),
			},
			want: VerdictDifferentSong,
		},
	})
}

func TestClassifyAudio_ProbeHardAndSoftAcrossResults(t *testing.T) {
	assertVerdicts(t, []verdictCase{
		{
			name: "the reference recording in a lower-scored result is still hard", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{
				acoustIDResult("top", 0.95, linked("live", "Don't Stop Me Now (live)", 210, "Queen")),
				acoustIDResult("low", 0.6, linked("canonical", "Don't Stop Me Now", 210, "Queen")),
			},
			want: VerdictHard,
		},
		{
			name: "the reference recording dropped by the length filter is not hard", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("canonical", "Bohemian Rhapsody", 355, "Queen")),
			want:    VerdictDifferentSong,
		},
		{
			name: "a soft sibling survives beside the reference recording dropped by length", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{acoustIDResult("q", 0.9,
				linked("canonical", "Don't Stop Me Now", 300, "Queen"),
				linked("sibling", "Don't Stop Me Now", 211, "Queen"))},
			want: VerdictSoft,
		},
		{
			name: "an empty but non-nil result list is unknown", ref: dontStopMeNow, audioDuration: 210,
			results: []ports.AcoustIDResult{},
			want:    VerdictUnknown,
		},
	})
}

func TestClassifyAudio_ProbeSurvivingCollectsLinksFromEveryResult(t *testing.T) {
	results := []ports.AcoustIDResult{
		acoustIDResult("a", 0.9, linked("a-keep", "Don't Stop Me Now", 210, "Queen"), linked("a-drop", "Don't Stop Me Now", 400, "Queen")),
		acoustIDResult("b", 0.8, linked("b-keep", "Don't Stop Me Now", 0, "Queen")),
	}

	verdict := ClassifyAudio(dontStopMeNow, 210, results)

	got := survivingMBIDs(verdict)
	slices.Sort(got)
	if want := []string{"a-keep", "b-keep"}; !slices.Equal(got, want) {
		t.Errorf("surviving links = %v, want %v", got, want)
	}
}

func TestClassifyAudio_ProbeSoftAgreementInputs(t *testing.T) {
	assertVerdicts(t, []verdictCase{
		{
			name: "a title with its apostrophe dropped agrees with the reference", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "Dont Stop Me Now", 210, "Queen")),
			want:    VerdictSoft,
		},
		{
			name: "title and artist case do not matter", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "DON'T STOP ME NOW", 210, "QUEEN")),
			want:    VerdictSoft,
		},
		{
			name: "a qualifier in square brackets is another version", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "Don't Stop Me Now [Live]", 210, "Queen")),
			want:    VerdictOtherVersion,
		},
		{
			name: "a recording without the reference's qualifier carries nothing unrequested",
			ref:  AudioReference{Title: "Nessun dorma (live)", Artist: "Luciano Pavarotti", Duration: 180}, audioDuration: 181,
			results: singleLink(linked("s", "Nessun dorma", 182, "Luciano Pavarotti")),
			want:    VerdictSoft,
		},
		{
			name: "a joint reference credit split on a comma overlaps one credited artist",
			ref:  AudioReference{Title: "Under Pressure", Artist: "Queen, David Bowie"}, audioDuration: 248,
			results: singleLink(linked("s", "Under Pressure", 248, "David Bowie")),
			want:    VerdictSoft,
		},
		{
			name: "any one of several recording artists may overlap", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "Don't Stop Me Now", 210, "Freddie Mercury", "Queen")),
			want:    VerdictSoft,
		},
		{
			name: "an artist whose name merely contains the reference artist does not overlap", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "Don't Stop Me Now", 210, "Queensryche")),
			want:    VerdictDifferentSong,
		},
		{
			name: "a recording with no artists never overlaps", ref: dontStopMeNow, audioDuration: 210,
			results: singleLink(linked("s", "Don't Stop Me Now", 210)),
			want:    VerdictDifferentSong,
		},
		{
			name: "an empty reference artist never overlaps an empty credit",
			ref:  AudioReference{Title: "Don't Stop Me Now", Artist: ""}, audioDuration: 210,
			results: singleLink(linked("s", "Don't Stop Me Now", 210, "")),
			want:    VerdictDifferentSong,
		},
		{
			name: "a reference length just inside five seconds of short audio agrees",
			ref:  AudioReference{Title: "Intro", Artist: "Queen", Duration: 105}, audioDuration: 100,
			results: singleLink(linked("s", "Intro", 100, "Queen")),
			want:    VerdictSoft,
		},
		{
			name: "a reference length just past five seconds of short audio disagrees",
			ref:  AudioReference{Title: "Intro", Artist: "Queen", Duration: 105.5}, audioDuration: 100,
			results: singleLink(linked("s", "Intro", 100, "Queen")),
			want:    VerdictDifferentSong,
		},
	})
}

func TestClassifyAudio_ProbeArtistSplitResistsHostileCredits(t *testing.T) {
	assertVerdicts(t, []verdictCase{
		{
			name: "feat inside an artist name does not split it into overlapping fragments",
			ref:  AudioReference{Title: "Wings", Artist: "Joe Feather"}, audioDuration: 200,
			results: singleLink(linked("s", "Wings", 200, "Her")),
			want:    VerdictDifferentSong,
		},
		{
			name: "a credit made only of separators never overlaps",
			ref:  AudioReference{Title: "Wings", Artist: " & , "}, audioDuration: 200,
			results: singleLink(linked("s", "Wings", 200, "&")),
			want:    VerdictDifferentSong,
		},
	})
}
