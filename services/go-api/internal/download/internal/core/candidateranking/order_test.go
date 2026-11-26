package candidateranking_test

import (
	"altune/go-api/internal/download"
	"slices"
	"testing"
)

func topicUpload(title string, duration float64, url string) download.AudioCandidate {
	return download.AudioCandidate{Title: title, Channel: "Artist - Topic", Duration: duration, URL: url, Categories: []string{"Music"}}
}

func TestRank_AnEqualTieGoesToTheLengthClosestToTheExpectedOne(t *testing.T) {
	track := song("Song", "Artist", 200)
	longer := topicUpload("Song", 240, "https://x.example/a")
	shorter := topicUpload("Song", 150, "https://x.example/b")

	ranked, _ := rank(track, shorter, longer)

	if got, want := urls(ranked), urls([]download.AudioCandidate{longer, shorter}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v: 40s off beats 50s off", got, want)
	}
}

func TestRank_WithoutAnExpectedLengthATieGoesByURL(t *testing.T) {
	track := song("Song", "Artist", 0)
	long := topicUpload("Song", 300, "https://x.example/a")
	short := topicUpload("Song", 100, "https://x.example/b")

	ranked, _ := rank(track, short, long)

	if got, want := urls(ranked), urls([]download.AudioCandidate{long, short}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v: no expected length makes every distance unknown", got, want)
	}
}

func TestRank_ACandidateWithoutALengthLosesATieToOneFarOff(t *testing.T) {
	track := song("Song", "Artist", 200)
	unmeasured := topicUpload("Song", 0, "https://x.example/a")
	farOff := topicUpload("Song", 450, "https://x.example/b")

	ranked, _ := rank(track, unmeasured, farOff)

	if got, want := urls(ranked), urls([]download.AudioCandidate{farOff, unmeasured}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v: an unknown length is the farthest of all", got, want)
	}
}

func TestRank_AnOtherVersionGoesAfterACleanCandidateThatLacksTheFeature(t *testing.T) {
	track := song("Song (feat. Guest)", "Artist", 200)
	clean := topicUpload("Song", 200, "https://x.example/clean")
	edit := topicUpload("Song (feat. Guest) (Radio Edit)", 200, "https://x.example/edit")

	ranked, refused := rank(track, edit, clean)

	if got, want := urls(ranked), urls([]download.AudioCandidate{clean, edit}); len(refused) != 0 || !slices.Equal(got, want) {
		t.Errorf("ranked = %v refused = %+v, want %v: a fallback qualifier outranks every other signal", got, refused, want)
	}
}

func TestRank_AnOtherVersionGoesLastOffTopicToo(t *testing.T) {
	track := song("Song", "Artist", 200)
	clean := download.AudioCandidate{Title: "Song", Channel: "Uploader", Duration: 260, URL: "https://x.example/clean"}
	edit := download.AudioCandidate{Title: "Artist - Song (Radio Edit)", Channel: "ArtistVEVO", Duration: 200, URL: "https://x.example/edit", Categories: []string{"Music"}}

	ranked, _ := rank(track, edit, clean)

	if got, want := urls(ranked), urls([]download.AudioCandidate{clean, edit}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OffTopicTheFeaturedCreditBreaksAnIdentityTie(t *testing.T) {
	track := song("Song (feat. Guest)", "Artist", 200)
	solo := download.AudioCandidate{Title: "Artist - Song (feat. Other)", Channel: "LabelVEVO", Duration: 200, URL: "https://x.example/a", Categories: []string{"Music"}, ViewCount: 9}
	featured := download.AudioCandidate{Title: "Artist - Song (feat. Guest)", Channel: "Uploader", Duration: 260, URL: "https://x.example/b", ViewCount: 1}
	records := evaluations(t, track, solo, featured)
	if records[0]["identity_score"] != records[1]["identity_score"] {
		t.Fatalf("identity scores %v and %v, want a tie so the feature decides", records[0]["identity_score"], records[1]["identity_score"])
	}

	ranked, _ := rank(track, solo, featured)

	if got, want := urls(ranked), urls([]download.AudioCandidate{featured, solo}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OnTopicTheFeaturedCreditBeatsAShorterQualifierList(t *testing.T) {
	track := song("Song (feat. Guest)", "Artist", 200)
	solo := topicUpload("Song", 200, "https://x.example/a")
	featured := topicUpload("Song (feat. Guest) (Official Audio) (Visualizer)", 200, "https://x.example/b")

	ranked, _ := rank(track, solo, featured)

	if got, want := urls(ranked), urls([]download.AudioCandidate{featured, solo}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OnTopicTheCloserIdentityWinsAtEqualQualifiers(t *testing.T) {
	track := song("Blinding Lights", "The Weeknd", 200)
	loose := download.AudioCandidate{Title: "Blinding Lights Extra Words Here", Channel: "The Weeknd - Topic", Duration: 200, URL: "https://x.example/a"}
	exact := download.AudioCandidate{Title: "The Weeknd - Blinding Lights", Channel: "The Weeknd - Topic", Duration: 260, URL: "https://x.example/b"}

	ranked, _ := rank(track, loose, exact)

	if got, want := urls(ranked), urls([]download.AudioCandidate{exact, loose}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OffTopicTheMetadataRankBreaksAnIdentityAndFeatureTie(t *testing.T) {
	track := song("Song", "Someone Else", 200)
	plain := download.AudioCandidate{Title: "Someone Else - Song", Channel: "Uploader", Duration: 200, URL: "https://x.example/a"}
	label := download.AudioCandidate{Title: "Someone Else - Song", Channel: "LabelVEVO", Duration: 200, URL: "https://x.example/b"}

	ranked, _ := rank(track, plain, label)

	if got, want := urls(ranked), urls([]download.AudioCandidate{label, plain}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OffTopicTheQualifierDistanceBreaksAMetadataTie(t *testing.T) {
	track := song("Song", "Someone Else", 200)
	video := download.AudioCandidate{Title: "Someone Else - Song (Video)", Channel: "Uploader", Duration: 200, URL: "https://x.example/a"}
	plain := download.AudioCandidate{Title: "Someone Else - Song (Audio)", Channel: "Uploader", Duration: 200, URL: "https://x.example/b"}
	records := evaluations(t, track, video, plain)
	if records[0]["identity_score"] != records[1]["identity_score"] {
		t.Fatalf("identity scores %v and %v, want a tie", records[0]["identity_score"], records[1]["identity_score"])
	}

	ranked, _ := rank(track, video, plain)

	if got, want := urls(ranked), urls([]download.AudioCandidate{plain, video}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_ResolvedCandidatesGoFirstThenTopicThenOther(t *testing.T) {
	track := song("Song", "Artist", 200)
	other := download.AudioCandidate{Title: "Artist - Song", Channel: "Uploader", Duration: 200, URL: "https://x.example/a"}
	topic := topicUpload("Song", 200, "https://x.example/b")
	art := download.AudioCandidate{Title: "Song", Duration: 200, URL: "https://x.example/c", Kind: download.KindArtTrack, Artists: []string{"Artist"}}

	ranked, _ := rank(track, other, topic, art)

	if got, want := urls(ranked), urls([]download.AudioCandidate{art, topic, other}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}
