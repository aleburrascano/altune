package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"testing"
)

var finesse = service.TitleTarget{Title: "Finesse", Artist: "Bruno Mars"}

func finesseCandidate(url string, kind ports.CandidateKind, seconds float64) ports.AudioCandidate {
	return downloadtest.Candidate(
		url,
		downloadtest.WithTitle("Finesse"),
		downloadtest.WithArtists([]string{"Bruno Mars"}),
		downloadtest.WithKind(kind),
		downloadtest.WithDuration(seconds),
	)
}

func wantComparison(t *testing.T, got service.ShadowComparison, pipelineURL, judgeURL string, verdict service.ShadowVerdict) {
	t.Helper()
	if got.PipelineURL != pipelineURL || got.JudgeURL != judgeURL || got.Verdict != verdict {
		t.Errorf("comparison = %+v, want pipeline %q, judge %q, verdict %s", got, pipelineURL, judgeURL, verdict)
	}
}

func TestCompare_TheCatalogPinIsJudgedAsThePinnedVideo(t *testing.T) {
	art := finesseCandidate("art", ports.KindArtTrack, 0)
	pin := finesseCandidate("pin", ports.KindCatalogPin, 0)

	got := service.CompareShadow(finesse, &pin, 0, []ports.AudioCandidate{art, pin})

	wantComparison(t, got, "pin", "pin", service.VerdictAgree)
	if got.Reason == "" || got.Reason == "no candidate accepted" {
		t.Errorf("reason = %q, want the judge's own reason for the candidate it chose", got.Reason)
	}
}

func TestCompare_AnAbstainingJudgeIsNotADisagreement(t *testing.T) {
	upload := finesseCandidate("https://music.youtube.com/watch?v=up", ports.KindUpload, 0)

	got := service.CompareShadow(finesse, &upload, 191, []ports.AudioCandidate{upload})

	wantComparison(t, got, upload.URL, "", service.VerdictJudgeAbstained)
	if got.Reason == "" {
		t.Error("an abstaining judge must say why")
	}
}

func TestCompare_OnlyTheKeptCandidateGainsTheProbedLength(t *testing.T) {
	target := service.TitleTarget{Title: "Finesse", Artist: "Bruno Mars", Length: 300}
	kept := finesseCandidate("kept", ports.KindArtTrack, 0)
	other := finesseCandidate("other", ports.KindArtTrack, 0)

	got := service.CompareShadow(target, &kept, 191, []ports.AudioCandidate{kept, other})

	wantComparison(t, got, "kept", "other", service.VerdictDisagree)
}

func TestCompare_APipelineThatKeptNothingFailed(t *testing.T) {
	art := finesseCandidate("art", ports.KindArtTrack, 0)

	got := service.CompareShadow(finesse, nil, 0, []ports.AudioCandidate{art})

	wantComparison(t, got, "", "art", service.VerdictPipelineFailed)
}
