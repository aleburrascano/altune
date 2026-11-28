package service_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"testing"
)

func TestSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []service.RejectionEntry
		want string
	}{
		{"none", nil, ""},
		{"one", []service.RejectionEntry{{Stage: service.RejectIdentity}}, "all 1 candidate rejected (1 identity)"},
		{
			"grouped and sorted",
			[]service.RejectionEntry{{Stage: service.RejectIdentity}, {Stage: service.RejectDuration}, {Stage: service.RejectIdentity}},
			"all 3 candidates rejected (1 duration, 2 identity)",
		},
		{
			"every stage that reaches a failure reason",
			[]service.RejectionEntry{
				{Stage: service.RejectFingerprint},
				{Stage: service.RejectDownload},
				{Stage: service.RejectUndecodable},
				{Stage: service.RejectDuration},
				{Stage: service.RejectIdentity},
				{Stage: service.RejectDownload},
			},
			"all 6 candidates rejected (2 download, 1 duration, 1 fingerprint, 1 identity, 1 undecodable)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.SummarizeRejections(tt.in); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummary_UntriedAndDuplicatesAreNotCountedAsRejected(t *testing.T) {
	tests := []struct {
		name string
		in   []service.RejectionEntry
		want string
	}{
		{"same source in both tiers counts once, last real stage wins", []service.RejectionEntry{
			{URL: "yt:a", Stage: service.RejectIdentity},
			{URL: "yt:a", Stage: service.RejectDownload},
		}, "all 1 candidate rejected (1 download)"},
		{"untried reported apart", []service.RejectionEntry{
			{URL: "yt:a", Stage: service.RejectDownload},
			{URL: "yt:b", Stage: service.RejectDownload},
			{URL: "yt:c", Stage: service.RejectNotAttempted},
			{URL: "yt:d", Stage: service.RejectNotAttempted},
			{URL: "yt:d", Stage: service.RejectNotAttempted},
		}, "all 2 candidates rejected (2 download), 2 not tried"},
		{"skipped after being judged stays judged", []service.RejectionEntry{
			{URL: "yt:a", Stage: service.RejectIdentity},
			{URL: "yt:a", Stage: service.RejectNotAttempted},
		}, "all 1 candidate rejected (1 identity)"},
		{"only untried", []service.RejectionEntry{
			{URL: "yt:a", Stage: service.RejectNotAttempted},
		}, "1 candidate not tried"},
		{"several untried", []service.RejectionEntry{
			{URL: "yt:a", Stage: service.RejectNotAttempted},
			{URL: "yt:b", Stage: service.RejectNotAttempted},
		}, "2 candidates not tried"},
		{"url-less entries count on their own", []service.RejectionEntry{
			{Stage: service.RejectIdentity}, {Stage: service.RejectIdentity},
		}, "all 2 candidates rejected (2 identity)"},
		{"two spellings of one video count once", []service.RejectionEntry{
			{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Stage: service.RejectIdentity},
			{URL: "https://music.youtube.com/watch?v=dQw4w9WgXcQ", Stage: service.RejectDuration},
		}, "all 1 candidate rejected (1 duration)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := service.SummarizeRejections(tt.in); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

const rejectedURL = "https://youtube.com/watch?v=aaaaaaaaaaa"

func TestStage_NamesStoredInAcquisitionRejectionsStayStable(t *testing.T) {
	stored := map[service.RejectionStage]string{
		service.RejectIdentity:    "identity",
		service.RejectQualifier:   "qualifier",
		service.RejectDRM:         "drm",
		service.RejectPreview:     "preview",
		service.RejectDuration:    "duration",
		service.RejectUndecodable: "undecodable",
		service.RejectFingerprint: "fingerprint",
	}
	for stage, reason := range stored {
		recs := service.LastingRejections([]service.RejectionEntry{{URL: rejectedURL, Stage: stage}}, "t1", domain.RejectionBasis{})
		if len(recs) != 1 || recs[0].Reason != reason {
			t.Errorf("stage %q stored as %+v, want one acquisition_rejections row with reason %q", stage, recs, reason)
		}
	}

	for _, stage := range []service.RejectionStage{service.RejectDownload, service.RejectNotAttempted, service.RejectCurrentFile} {
		if recs := service.LastingRejections([]service.RejectionEntry{{URL: rejectedURL, Stage: stage}}, "t1", domain.RejectionBasis{}); len(recs) != 0 {
			t.Errorf("stage %q stored as %+v, want no row: it says nothing lasting about the source", stage, recs)
		}
	}
}

func TestLasting_KeepsFingerprintAndDropsTransientStages(t *testing.T) {
	entries := []service.RejectionEntry{
		{URL: "https://youtube.com/watch?v=aaaaaaaaaaa", Stage: service.RejectFingerprint, Reason: "other version"},
		{URL: "https://youtube.com/watch?v=bbbbbbbbbbb", Stage: service.RejectDownload, Reason: "download failed"},
		{URL: "https://youtube.com/watch?v=ccccccccccc", Stage: service.RejectNotAttempted, Reason: "skipped"},
	}

	recs := service.LastingRejections(entries, "t1", domain.RejectionBasis{})

	if len(recs) != 1 || recs[0].Reason != "fingerprint" || recs[0].TrackID != "t1" ||
		recs[0].SourceKey != domain.SourceKey("https://youtube.com/watch?v=aaaaaaaaaaa") || recs[0].Detail != "other version" {
		t.Errorf("records = %+v, want only the fingerprint rejection", recs)
	}
}

func TestLasting_CarriesTheRunsBasis(t *testing.T) {
	basis := domain.RejectionBasis{ExpectedSeconds: 322, ReferenceMBID: "mbid-1"}

	recs := service.LastingRejections([]service.RejectionEntry{{URL: "https://youtube.com/watch?v=aaaaaaaaaaa", Stage: service.RejectDuration, Reason: "too short"}}, "t1", basis)

	if len(recs) != 1 || recs[0].Basis != basis {
		t.Errorf("records = %+v, want one carrying basis %+v", recs, basis)
	}
}

func TestLasting_DropsAnUnmeasuredDuration(t *testing.T) {
	entries := []service.RejectionEntry{
		{URL: "https://youtube.com/watch?v=aaaaaaaaaaa", Stage: service.RejectDuration, Reason: "search duration 30s vs expected 200s", Unmeasured: true},
		{URL: "https://youtube.com/watch?v=bbbbbbbbbbb", Stage: service.RejectDuration, Reason: "duration 30s vs expected 200s"},
	}

	recs := service.LastingRejections(entries, "t1", domain.RejectionBasis{})

	if len(recs) != 1 || recs[0].SourceKey != domain.SourceKey("https://youtube.com/watch?v=bbbbbbbbbbb") {
		t.Errorf("records = %+v, want only the measured duration rejection", recs)
	}
}

func TestLasting_DropsAnEntryWithNoSourceKey(t *testing.T) {
	if recs := service.LastingRejections([]service.RejectionEntry{{Stage: service.RejectFingerprint}}, "t1", domain.RejectionBasis{}); len(recs) != 0 {
		t.Errorf("records = %+v, want none: a row needs a source key", recs)
	}
}
