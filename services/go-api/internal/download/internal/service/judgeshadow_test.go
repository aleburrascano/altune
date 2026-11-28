package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"testing"
)

var finesse = service.TitleTarget{Title: "Finesse", Artist: "Bruno Mars"}

func finesseCandidate(url string, kind domain.CandidateKind, seconds float64) domain.AudioCandidate {
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
	art := finesseCandidate("art", domain.KindArtTrack, 0)
	pin := finesseCandidate("pin", domain.KindCatalogPin, 0)

	got := service.CompareShadow(finesse, &pin, 0, []domain.AudioCandidate{art, pin})

	wantComparison(t, got, "pin", "pin", service.VerdictAgree)
	if got.Reason == "" || got.Reason == "no candidate accepted" {
		t.Errorf("reason = %q, want the judge's own reason for the candidate it chose", got.Reason)
	}
}

func TestCompare_AnAbstainingJudgeIsNotADisagreement(t *testing.T) {
	upload := finesseCandidate("https://music.youtube.com/watch?v=up", domain.KindUpload, 0)

	got := service.CompareShadow(finesse, &upload, 191, []domain.AudioCandidate{upload})

	wantComparison(t, got, upload.URL, "", service.VerdictJudgeAbstained)
	if got.Reason == "" {
		t.Error("an abstaining judge must say why")
	}
}

func TestCompare_OnlyTheKeptCandidateGainsTheProbedLength(t *testing.T) {
	target := service.TitleTarget{Title: "Finesse", Artist: "Bruno Mars", Length: 300}
	kept := finesseCandidate("kept", domain.KindArtTrack, 0)
	other := finesseCandidate("other", domain.KindArtTrack, 0)

	got := service.CompareShadow(target, &kept, 191, []domain.AudioCandidate{kept, other})

	wantComparison(t, got, "kept", "other", service.VerdictDisagree)
}

func TestCompare_APipelineThatKeptNothingFailed(t *testing.T) {
	art := finesseCandidate("art", domain.KindArtTrack, 0)

	got := service.CompareShadow(finesse, nil, 0, []domain.AudioCandidate{art})

	wantComparison(t, got, "", "art", service.VerdictPipelineFailed)
}
