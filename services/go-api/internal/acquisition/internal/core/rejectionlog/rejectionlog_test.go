package rejectionlog_test

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/rejectionlog"
	"altune/go-api/internal/acquisition/sourcekey"
	"testing"
)

func TestSummary(t *testing.T) {
	tests := []struct {
		name string
		in   []rejectionlog.Entry
		want string
	}{
		{"none", nil, ""},
		{"one", []rejectionlog.Entry{{Stage: rejectionlog.Identity}}, "all 1 candidate rejected (1 identity)"},
		{
			"grouped and sorted",
			[]rejectionlog.Entry{{Stage: rejectionlog.Identity}, {Stage: rejectionlog.Duration}, {Stage: rejectionlog.Identity}},
			"all 3 candidates rejected (1 duration, 2 identity)",
		},
		{
			"every stage that reaches a failure reason",
			[]rejectionlog.Entry{
				{Stage: rejectionlog.Fingerprint},
				{Stage: rejectionlog.Download},
				{Stage: rejectionlog.Undecodable},
				{Stage: rejectionlog.Duration},
				{Stage: rejectionlog.Identity},
				{Stage: rejectionlog.Download},
			},
			"all 6 candidates rejected (2 download, 1 duration, 1 fingerprint, 1 identity, 1 undecodable)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rejectionlog.Summary(tt.in); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSummary_UntriedAndDuplicatesAreNotCountedAsRejected(t *testing.T) {
	tests := []struct {
		name string
		in   []rejectionlog.Entry
		want string
	}{
		{"same source in both tiers counts once, last real stage wins", []rejectionlog.Entry{
			{URL: "yt:a", Stage: rejectionlog.Identity},
			{URL: "yt:a", Stage: rejectionlog.Download},
		}, "all 1 candidate rejected (1 download)"},
		{"untried reported apart", []rejectionlog.Entry{
			{URL: "yt:a", Stage: rejectionlog.Download},
			{URL: "yt:b", Stage: rejectionlog.Download},
			{URL: "yt:c", Stage: rejectionlog.NotAttempted},
			{URL: "yt:d", Stage: rejectionlog.NotAttempted},
			{URL: "yt:d", Stage: rejectionlog.NotAttempted},
		}, "all 2 candidates rejected (2 download), 2 not tried"},
		{"skipped after being judged stays judged", []rejectionlog.Entry{
			{URL: "yt:a", Stage: rejectionlog.Identity},
			{URL: "yt:a", Stage: rejectionlog.NotAttempted},
		}, "all 1 candidate rejected (1 identity)"},
		{"only untried", []rejectionlog.Entry{
			{URL: "yt:a", Stage: rejectionlog.NotAttempted},
		}, "1 candidate not tried"},
		{"several untried", []rejectionlog.Entry{
			{URL: "yt:a", Stage: rejectionlog.NotAttempted},
			{URL: "yt:b", Stage: rejectionlog.NotAttempted},
		}, "2 candidates not tried"},
		{"url-less entries count on their own", []rejectionlog.Entry{
			{Stage: rejectionlog.Identity}, {Stage: rejectionlog.Identity},
		}, "all 2 candidates rejected (2 identity)"},
		{"two spellings of one video count once", []rejectionlog.Entry{
			{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Stage: rejectionlog.Identity},
			{URL: "https://music.youtube.com/watch?v=dQw4w9WgXcQ", Stage: rejectionlog.Duration},
		}, "all 1 candidate rejected (1 duration)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rejectionlog.Summary(tt.in); got != tt.want {
				t.Errorf("Summary = %q, want %q", got, tt.want)
			}
		})
	}
}

const rejectedURL = "https://youtube.com/watch?v=aaaaaaaaaaa"

func TestStage_NamesStoredInAcquisitionRejectionsStayStable(t *testing.T) {
	stored := map[rejectionlog.Stage]string{
		rejectionlog.Identity:    "identity",
		rejectionlog.Qualifier:   "qualifier",
		rejectionlog.DRM:         "drm",
		rejectionlog.Preview:     "preview",
		rejectionlog.Duration:    "duration",
		rejectionlog.Undecodable: "undecodable",
		rejectionlog.Fingerprint: "fingerprint",
	}
	for stage, reason := range stored {
		recs := rejectionlog.Lasting([]rejectionlog.Entry{{URL: rejectedURL, Stage: stage}}, "t1", ports.RejectionBasis{})
		if len(recs) != 1 || recs[0].Reason != reason {
			t.Errorf("stage %q stored as %+v, want one acquisition_rejections row with reason %q", stage, recs, reason)
		}
	}

	for _, stage := range []rejectionlog.Stage{rejectionlog.Download, rejectionlog.NotAttempted, rejectionlog.CurrentFile} {
		if recs := rejectionlog.Lasting([]rejectionlog.Entry{{URL: rejectedURL, Stage: stage}}, "t1", ports.RejectionBasis{}); len(recs) != 0 {
			t.Errorf("stage %q stored as %+v, want no row: it says nothing lasting about the source", stage, recs)
		}
	}
}

func TestLasting_KeepsFingerprintAndDropsTransientStages(t *testing.T) {
	entries := []rejectionlog.Entry{
		{URL: "https://youtube.com/watch?v=a", Stage: rejectionlog.Fingerprint, Reason: "other version"},
		{URL: "https://youtube.com/watch?v=b", Stage: rejectionlog.Download, Reason: "download failed"},
		{URL: "https://youtube.com/watch?v=c", Stage: rejectionlog.NotAttempted, Reason: "skipped"},
	}

	recs := rejectionlog.Lasting(entries, "t1", ports.RejectionBasis{})

	if len(recs) != 1 || recs[0].Reason != "fingerprint" || recs[0].TrackID != "t1" ||
		recs[0].SourceKey != sourcekey.SourceKey("https://youtube.com/watch?v=a") || recs[0].Detail != "other version" {
		t.Errorf("records = %+v, want only the fingerprint rejection", recs)
	}
}

func TestLasting_CarriesTheRunsBasis(t *testing.T) {
	basis := ports.RejectionBasis{ExpectedSeconds: 322, ReferenceMBID: "mbid-1"}

	recs := rejectionlog.Lasting([]rejectionlog.Entry{{URL: "https://youtube.com/watch?v=a", Stage: rejectionlog.Duration, Reason: "too short"}}, "t1", basis)

	if len(recs) != 1 || recs[0].Basis != basis {
		t.Errorf("records = %+v, want one carrying basis %+v", recs, basis)
	}
}

func TestLasting_DropsAnUnmeasuredDuration(t *testing.T) {
	entries := []rejectionlog.Entry{
		{URL: "https://youtube.com/watch?v=a", Stage: rejectionlog.Duration, Reason: "search duration 30s vs expected 200s", Unmeasured: true},
		{URL: "https://youtube.com/watch?v=b", Stage: rejectionlog.Duration, Reason: "duration 30s vs expected 200s"},
	}

	recs := rejectionlog.Lasting(entries, "t1", ports.RejectionBasis{})

	if len(recs) != 1 || recs[0].SourceKey != sourcekey.SourceKey("https://youtube.com/watch?v=b") {
		t.Errorf("records = %+v, want only the measured duration rejection", recs)
	}
}

func TestLasting_DropsAnEntryWithNoSourceKey(t *testing.T) {
	if recs := rejectionlog.Lasting([]rejectionlog.Entry{{Stage: rejectionlog.Fingerprint}}, "t1", ports.RejectionBasis{}); len(recs) != 0 {
		t.Errorf("records = %+v, want none: a row needs a source key", recs)
	}
}
