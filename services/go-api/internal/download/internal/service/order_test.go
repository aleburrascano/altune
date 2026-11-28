package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/ports"
	"slices"
	"testing"
)

func rankTopicUpload(title string, duration float64, url string) ports.AudioCandidate {
	return downloadtest.Candidate(
		url,
		downloadtest.WithTitle(title),
		downloadtest.WithChannel("Artist - Topic"),
		downloadtest.WithDuration(duration),
		downloadtest.WithCategories([]string{"Music"}),
	)
}

func TestRank_AnEqualTieGoesToTheLengthClosestToTheExpectedOne(t *testing.T) {
	track := song("Song", "Artist", 200)
	longer := rankTopicUpload("Song", 240, "https://x.example/a")
	shorter := rankTopicUpload("Song", 150, "https://x.example/b")

	ranked, _ := rank(track, shorter, longer)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{longer, shorter}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v: 40s off beats 50s off", got, want)
	}
}

func TestRank_WithoutAnExpectedLengthATieGoesByURL(t *testing.T) {
	track := song("Song", "Artist", 0)
	long := rankTopicUpload("Song", 300, "https://x.example/a")
	short := rankTopicUpload("Song", 100, "https://x.example/b")

	ranked, _ := rank(track, short, long)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{long, short}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v: no expected length makes every distance unknown", got, want)
	}
}

func TestRank_ACandidateWithoutALengthLosesATieToOneFarOff(t *testing.T) {
	track := song("Song", "Artist", 200)
	unmeasured := rankTopicUpload("Song", 0, "https://x.example/a")
	farOff := rankTopicUpload("Song", 450, "https://x.example/b")

	ranked, _ := rank(track, unmeasured, farOff)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{farOff, unmeasured}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v: an unknown length is the farthest of all", got, want)
	}
}

func TestRank_AnOtherVersionGoesAfterACleanCandidateThatLacksTheFeature(t *testing.T) {
	track := song("Song (feat. Guest)", "Artist", 200)
	clean := rankTopicUpload("Song", 200, "https://x.example/clean")
	edit := rankTopicUpload("Song (feat. Guest) (Radio Edit)", 200, "https://x.example/edit")

	ranked, refused := rank(track, edit, clean)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{clean, edit}); len(refused) != 0 || !slices.Equal(got, want) {
		t.Errorf("ranked = %v refused = %+v, want %v: a fallback qualifier outranks every other signal", got, refused, want)
	}
}

func TestRank_AnOtherVersionGoesLastOffTopicToo(t *testing.T) {
	track := song("Song", "Artist", 200)
	clean := downloadtest.Candidate(
		"https://x.example/clean",
		downloadtest.WithTitle("Song"),
		downloadtest.WithChannel("Uploader"),
		downloadtest.WithDuration(260),
	)
	edit := downloadtest.Candidate(
		"https://x.example/edit",
		downloadtest.WithTitle("Artist - Song (Radio Edit)"),
		downloadtest.WithChannel("ArtistVEVO"),
		downloadtest.WithDuration(200),
		downloadtest.WithCategories([]string{"Music"}),
	)

	ranked, _ := rank(track, edit, clean)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{clean, edit}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OffTopicTheFeaturedCreditBreaksAnIdentityTie(t *testing.T) {
	track := song("Song (feat. Guest)", "Artist", 200)
	solo := downloadtest.Candidate(
		"https://x.example/a",
		downloadtest.WithTitle("Artist - Song (feat. Other)"),
		downloadtest.WithChannel("LabelVEVO"),
		downloadtest.WithDuration(200),
		downloadtest.WithCategories([]string{"Music"}),
		downloadtest.WithViewCount(9),
	)
	featured := downloadtest.Candidate(
		"https://x.example/b",
		downloadtest.WithTitle("Artist - Song (feat. Guest)"),
		downloadtest.WithChannel("Uploader"),
		downloadtest.WithDuration(260),
		downloadtest.WithViewCount(1),
	)
	records := evaluations(t, track, solo, featured)
	if records[0]["identity_score"] != records[1]["identity_score"] {
		t.Fatalf("identity scores %v and %v, want a tie so the feature decides", records[0]["identity_score"], records[1]["identity_score"])
	}

	ranked, _ := rank(track, solo, featured)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{featured, solo}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OnTopicTheFeaturedCreditBeatsAShorterQualifierList(t *testing.T) {
	track := song("Song (feat. Guest)", "Artist", 200)
	solo := rankTopicUpload("Song", 200, "https://x.example/a")
	featured := rankTopicUpload("Song (feat. Guest) (Official Audio) (Visualizer)", 200, "https://x.example/b")

	ranked, _ := rank(track, solo, featured)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{featured, solo}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OnTopicTheCloserIdentityWinsAtEqualQualifiers(t *testing.T) {
	track := song("Blinding Lights", "The Weeknd", 200)
	loose := downloadtest.Candidate(
		"https://x.example/a",
		downloadtest.WithTitle("Blinding Lights Extra Words Here"),
		downloadtest.WithChannel("The Weeknd - Topic"),
		downloadtest.WithDuration(200),
	)
	exact := downloadtest.Candidate(
		"https://x.example/b",
		downloadtest.WithTitle("The Weeknd - Blinding Lights"),
		downloadtest.WithChannel("The Weeknd - Topic"),
		downloadtest.WithDuration(260),
	)

	ranked, _ := rank(track, loose, exact)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{exact, loose}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OffTopicTheMetadataRankBreaksAnIdentityAndFeatureTie(t *testing.T) {
	track := song("Song", "Someone Else", 200)
	plain := downloadtest.Candidate(
		"https://x.example/a",
		downloadtest.WithTitle("Someone Else - Song"),
		downloadtest.WithChannel("Uploader"),
		downloadtest.WithDuration(200),
	)
	label := downloadtest.Candidate(
		"https://x.example/b",
		downloadtest.WithTitle("Someone Else - Song"),
		downloadtest.WithChannel("LabelVEVO"),
		downloadtest.WithDuration(200),
	)

	ranked, _ := rank(track, plain, label)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{label, plain}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_OffTopicTheQualifierDistanceBreaksAMetadataTie(t *testing.T) {
	track := song("Song", "Someone Else", 200)
	video := downloadtest.Candidate(
		"https://x.example/a",
		downloadtest.WithTitle("Someone Else - Song (Video)"),
		downloadtest.WithChannel("Uploader"),
		downloadtest.WithDuration(200),
	)
	plain := downloadtest.Candidate(
		"https://x.example/b",
		downloadtest.WithTitle("Someone Else - Song (Audio)"),
		downloadtest.WithChannel("Uploader"),
		downloadtest.WithDuration(200),
	)
	records := evaluations(t, track, video, plain)
	if records[0]["identity_score"] != records[1]["identity_score"] {
		t.Fatalf("identity scores %v and %v, want a tie", records[0]["identity_score"], records[1]["identity_score"])
	}

	ranked, _ := rank(track, video, plain)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{plain, video}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}

func TestRank_ResolvedCandidatesGoFirstThenTopicThenOther(t *testing.T) {
	track := song("Song", "Artist", 200)
	other := downloadtest.Candidate(
		"https://x.example/a",
		downloadtest.WithTitle("Artist - Song"),
		downloadtest.WithChannel("Uploader"),
		downloadtest.WithDuration(200),
	)
	topic := rankTopicUpload("Song", 200, "https://x.example/b")
	art := downloadtest.Candidate(
		"https://x.example/c",
		downloadtest.WithTitle("Song"),
		downloadtest.WithDuration(200),
		downloadtest.WithKind(ports.KindArtTrack),
		downloadtest.WithArtists([]string{"Artist"}),
	)

	ranked, _ := rank(track, other, topic, art)

	if got, want := urls(ranked), urls([]ports.AudioCandidate{art, topic, other}); !slices.Equal(got, want) {
		t.Errorf("ranked = %v, want %v", got, want)
	}
}
