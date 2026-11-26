package rejectedcandidates_test

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"altune/go-api/internal/download/sourcekey"
	"testing"
)

func TestSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []rejectedcandidates.Entry
		want string
	}{
		{"none", nil, ""},
		{"one", []rejectedcandidates.Entry{{Stage: rejectedcandidates.Identity}}, "all 1 candidate rejected (1 identity)"},
		{
			"grouped and sorted",
			[]rejectedcandidates.Entry{{Stage: rejectedcandidates.Identity}, {Stage: rejectedcandidates.Duration}, {Stage: rejectedcandidates.Identity}},
			"all 3 candidates rejected (1 duration, 2 identity)",
		},
		{
			"every stage that reaches a failure reason",
			[]rejectedcandidates.Entry{
				{Stage: rejectedcandidates.Fingerprint},
				{Stage: rejectedcandidates.Download},
				{Stage: rejectedcandidates.Undecodable},
				{Stage: rejectedcandidates.Duration},
				{Stage: rejectedcandidates.Identity},
				{Stage: rejectedcandidates.Download},
			},
			"all 6 candidates rejected (2 download, 1 duration, 1 fingerprint, 1 identity, 1 undecodable)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rejectedcandidates.Summary(tt.in); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummary_UntriedAndDuplicatesAreNotCountedAsRejected(t *testing.T) {
	tests := []struct {
		name string
		in   []rejectedcandidates.Entry
		want string
	}{
		{"same source in both tiers counts once, last real stage wins", []rejectedcandidates.Entry{
			{URL: "yt:a", Stage: rejectedcandidates.Identity},
			{URL: "yt:a", Stage: rejectedcandidates.Download},
		}, "all 1 candidate rejected (1 download)"},
		{"untried reported apart", []rejectedcandidates.Entry{
			{URL: "yt:a", Stage: rejectedcandidates.Download},
			{URL: "yt:b", Stage: rejectedcandidates.Download},
			{URL: "yt:c", Stage: rejectedcandidates.NotAttempted},
			{URL: "yt:d", Stage: rejectedcandidates.NotAttempted},
			{URL: "yt:d", Stage: rejectedcandidates.NotAttempted},
		}, "all 2 candidates rejected (2 download), 2 not tried"},
		{"skipped after being judged stays judged", []rejectedcandidates.Entry{
			{URL: "yt:a", Stage: rejectedcandidates.Identity},
			{URL: "yt:a", Stage: rejectedcandidates.NotAttempted},
		}, "all 1 candidate rejected (1 identity)"},
		{"only untried", []rejectedcandidates.Entry{
			{URL: "yt:a", Stage: rejectedcandidates.NotAttempted},
		}, "1 candidate not tried"},
		{"several untried", []rejectedcandidates.Entry{
			{URL: "yt:a", Stage: rejectedcandidates.NotAttempted},
			{URL: "yt:b", Stage: rejectedcandidates.NotAttempted},
		}, "2 candidates not tried"},
		{"url-less entries count on their own", []rejectedcandidates.Entry{
			{Stage: rejectedcandidates.Identity}, {Stage: rejectedcandidates.Identity},
		}, "all 2 candidates rejected (2 identity)"},
		{"two spellings of one video count once", []rejectedcandidates.Entry{
			{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Stage: rejectedcandidates.Identity},
			{URL: "https://music.youtube.com/watch?v=dQw4w9WgXcQ", Stage: rejectedcandidates.Duration},
		}, "all 1 candidate rejected (1 duration)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rejectedcandidates.Summary(tt.in); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

const rejectedURL = "https://youtube.com/watch?v=aaaaaaaaaaa"

func TestStage_NamesStoredInAcquisitionRejectionsStayStable(t *testing.T) {
	stored := map[rejectedcandidates.Stage]string{
		rejectedcandidates.Identity:    "identity",
		rejectedcandidates.Qualifier:   "qualifier",
		rejectedcandidates.DRM:         "drm",
		rejectedcandidates.Preview:     "preview",
		rejectedcandidates.Duration:    "duration",
		rejectedcandidates.Undecodable: "undecodable",
		rejectedcandidates.Fingerprint: "fingerprint",
	}
	for stage, reason := range stored {
		recs := rejectedcandidates.Lasting([]rejectedcandidates.Entry{{URL: rejectedURL, Stage: stage}}, "t1", ports.RejectionBasis{})
		if len(recs) != 1 || recs[0].Reason != reason {
			t.Errorf("stage %q stored as %+v, want one acquisition_rejections row with reason %q", stage, recs, reason)
		}
	}

	for _, stage := range []rejectedcandidates.Stage{rejectedcandidates.Download, rejectedcandidates.NotAttempted, rejectedcandidates.CurrentFile} {
		if recs := rejectedcandidates.Lasting([]rejectedcandidates.Entry{{URL: rejectedURL, Stage: stage}}, "t1", ports.RejectionBasis{}); len(recs) != 0 {
			t.Errorf("stage %q stored as %+v, want no row: it says nothing lasting about the source", stage, recs)
		}
	}
}

func TestLasting_KeepsFingerprintAndDropsTransientStages(t *testing.T) {
	entries := []rejectedcandidates.Entry{
		{URL: "https://youtube.com/watch?v=aaaaaaaaaaa", Stage: rejectedcandidates.Fingerprint, Reason: "other version"},
		{URL: "https://youtube.com/watch?v=bbbbbbbbbbb", Stage: rejectedcandidates.Download, Reason: "download failed"},
		{URL: "https://youtube.com/watch?v=ccccccccccc", Stage: rejectedcandidates.NotAttempted, Reason: "skipped"},
	}

	recs := rejectedcandidates.Lasting(entries, "t1", ports.RejectionBasis{})

	if len(recs) != 1 || recs[0].Reason != "fingerprint" || recs[0].TrackID != "t1" ||
		recs[0].SourceKey != sourcekey.SourceKey("https://youtube.com/watch?v=aaaaaaaaaaa") || recs[0].Detail != "other version" {
		t.Errorf("records = %+v, want only the fingerprint rejection", recs)
	}
}

func TestLasting_CarriesTheRunsBasis(t *testing.T) {
	basis := ports.RejectionBasis{ExpectedSeconds: 322, ReferenceMBID: "mbid-1"}

	recs := rejectedcandidates.Lasting([]rejectedcandidates.Entry{{URL: "https://youtube.com/watch?v=aaaaaaaaaaa", Stage: rejectedcandidates.Duration, Reason: "too short"}}, "t1", basis)

	if len(recs) != 1 || recs[0].Basis != basis {
		t.Errorf("records = %+v, want one carrying basis %+v", recs, basis)
	}
}

func TestLasting_DropsAnUnmeasuredDuration(t *testing.T) {
	entries := []rejectedcandidates.Entry{
		{URL: "https://youtube.com/watch?v=aaaaaaaaaaa", Stage: rejectedcandidates.Duration, Reason: "search duration 30s vs expected 200s", Unmeasured: true},
		{URL: "https://youtube.com/watch?v=bbbbbbbbbbb", Stage: rejectedcandidates.Duration, Reason: "duration 30s vs expected 200s"},
	}

	recs := rejectedcandidates.Lasting(entries, "t1", ports.RejectionBasis{})

	if len(recs) != 1 || recs[0].SourceKey != sourcekey.SourceKey("https://youtube.com/watch?v=bbbbbbbbbbb") {
		t.Errorf("records = %+v, want only the measured duration rejection", recs)
	}
}

func TestLasting_DropsAnEntryWithNoSourceKey(t *testing.T) {
	if recs := rejectedcandidates.Lasting([]rejectedcandidates.Entry{{Stage: rejectedcandidates.Fingerprint}}, "t1", ports.RejectionBasis{}); len(recs) != 0 {
		t.Errorf("records = %+v, want none: a row needs a source key", recs)
	}
}
