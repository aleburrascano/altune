package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRecordingIdentity_SourceFor(t *testing.T) {
	id := ports.RecordingIdentity{Sources: []ports.ProviderRef{
		{Provider: "deezer", ExternalID: "123"},
		{Provider: "youtube", ExternalID: "abc"},
	}}

	if got, ok := id.SourceFor("youtube"); !ok || got.ExternalID != "abc" {
		t.Errorf("SourceFor(youtube) = %+v %v", got, ok)
	}
	if _, ok := id.SourceFor("tidal"); ok {
		t.Error("SourceFor(tidal) should report missing")
	}
}

type clusterErrIdentifier struct{ err error }

func (clusterErrIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	return ports.RecordingMatch{}, nil
}

func (i clusterErrIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return nil, i.err
}

func TestResolveIdentity_ClusterLookupErrorLogEvent(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		want      string
		notWanted string
	}{
		{
			name:      "throttled",
			err:       fmt.Errorf("acoustid lookup: %w", ports.ErrIdentifyThrottled),
			want:      "acquisition.identify_throttled",
			notWanted: "acquisition.expected_cluster_failed",
		},
		{
			name:      "other failure",
			err:       errors.New("boom"),
			want:      "acquisition.expected_cluster_failed",
			notWanted: "acquisition.identify_throttled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureJSONLog(t)
			ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}}
			resolver := &stubResolver{identity: ports.RecordingIdentity{MBID: "mb-1"}}

			ResolveIdentity(context.Background(), resolver, clusterErrIdentifier{err: tc.err}, ac)

			rec := findLogRecord(t, logs, tc.want)
			if rec["track_id"] != "t1" || rec["mbid"] != "mb-1" {
				t.Errorf("log attrs = %v", rec)
			}
			if strings.Contains(logs.String(), tc.notWanted) {
				t.Errorf("unexpected %s: %s", tc.notWanted, logs.String())
			}
			if len(ac.Identity.AcoustIDs) != 0 {
				t.Errorf("AcoustIDs = %v, want empty", ac.Identity.AcoustIDs)
			}
		})
	}
}
