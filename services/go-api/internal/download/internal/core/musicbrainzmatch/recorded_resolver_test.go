package musicbrainzmatch_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/musicbrainzmatch"
	"context"
	"slices"
	"testing"
)

func TestNewRecorded_ServesThePinnedFactsForThePinnedMBIDAlone(t *testing.T) {
	resolver := musicbrainzmatch.NewRecorded(nil, nil, &cureStudioFacts)

	facts, found, err := resolver.RecordingByMBID(context.Background(), cureStudioMBID)
	if err != nil || !found || facts.Duration != cureStudioFacts.Duration {
		t.Errorf("pinned mbid = %+v, %v, %v; want the recorded facts", facts, found, err)
	}
	if _, found, _ := resolver.RecordingByMBID(context.Background(), cureLiveMBID); found {
		t.Error("another mbid must not be served the pinned facts")
	}
	if _, found, _ := musicbrainzmatch.NewRecorded(nil, nil, nil).RecordingByMBID(context.Background(), cureStudioMBID); found {
		t.Error("a recording with no pin must serve no facts")
	}
}

func TestNewRecorded_ResolvesFromTheRecordedSearchAndISRCAnswers(t *testing.T) {
	hits := []download.SearchedRecording{{Title: "Drinking in L.A.", Artist: "Bran Van 3000", ISRC: "CAA509814003", MBID: "remix-mbid", Duration: 236}}
	isrc := []download.ISRCRecording{{MBID: "album-version", Duration: 237}}

	identity, err := musicbrainzmatch.NewRecorded(hits, isrc, nil).Resolve(context.Background(), drinkingQuery)

	if err != nil || identity.MBID != "album-version" || !slices.Equal(identity.MBIDs, []string{"album-version"}) {
		t.Errorf("identity %+v, err %v; want the recorded ISRC recording to anchor the recorded search hit", identity, err)
	}
	if hits[0].MBID != "remix-mbid" || isrc[0].MBID != "album-version" {
		t.Errorf("recorded answers changed under the resolver: %+v %+v", hits, isrc)
	}
}
