package catalogbridge

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"slices"
	"testing"
)

func seededSnapshot(t *testing.T, seed SeededTrack) (*SeededLedger, *ports.TrackSnapshot) {
	t.Helper()
	ledger, err := NewSeededLedger(seed)
	if err != nil {
		t.Fatalf("NewSeededLedger: %v", err)
	}
	snapshot, err := ledger.Get(context.Background(), ledger.TrackID(), ledger.UserID())
	if err != nil || snapshot == nil {
		t.Fatalf("Get = %v, %v", snapshot, err)
	}
	return ledger, snapshot
}

func TestNewSeededLedger_SnapshotCarriesTheSeededFields(t *testing.T) {
	explicit := true
	sources := []ports.CatalogSource{{Provider: "musicbrainz", ExternalID: "abc"}}

	_, snapshot := seededSnapshot(t, SeededTrack{
		Title: "Over", Artist: "LUCKI", Album: "", ISRC: "CA5KR1827984",
		CatalogDuration: 135.96, CatalogMBID: "pinned", CatalogExplicit: &explicit,
		CatalogSources: sources, RejectedSourceKeys: []string{"youtube:FjkkqbEbFOY"},
	})

	if snapshot.Title != "Over" || snapshot.Artist != "LUCKI" || snapshot.Album != "" {
		t.Errorf("identity = %q/%q/%q, want the album left as seeded", snapshot.Title, snapshot.Artist, snapshot.Album)
	}
	if snapshot.ISRC == nil || *snapshot.ISRC != "CA5KR1827984" {
		t.Errorf("ISRC = %v", snapshot.ISRC)
	}
	if snapshot.CatalogDurationSeconds == nil || *snapshot.CatalogDurationSeconds != 135.96 {
		t.Errorf("CatalogDurationSeconds = %v, want the catalog length", snapshot.CatalogDurationSeconds)
	}
	if snapshot.CatalogMBID == nil || *snapshot.CatalogMBID != "pinned" {
		t.Errorf("CatalogMBID = %v", snapshot.CatalogMBID)
	}
	if snapshot.CatalogExplicit == nil || !*snapshot.CatalogExplicit {
		t.Errorf("CatalogExplicit = %v", snapshot.CatalogExplicit)
	}
	if !slices.Equal(snapshot.CatalogSources, sources) {
		t.Errorf("CatalogSources = %+v", snapshot.CatalogSources)
	}
	if !slices.Equal(snapshot.RejectedSourceKeys, []string{"youtube:FjkkqbEbFOY"}) {
		t.Errorf("RejectedSourceKeys = %v", snapshot.RejectedSourceKeys)
	}
	if snapshot.Status != ports.TrackPending {
		t.Errorf("Status = %v, want pending without a stored file", snapshot.Status)
	}
}

func TestNewSeededLedger_AStoredFileMakesTheTrackReadyWithItsMeasuredLength(t *testing.T) {
	_, snapshot := seededSnapshot(t, SeededTrack{
		Title: "Phone (Interlude)", Artist: "SLAYR",
		Stored: &SeededFile{Duration: 112, SourceURL: "https://www.youtube.com/watch?v=TB0z2HVFxEE"},
	})

	if snapshot.Status != ports.TrackReady || snapshot.AudioRef == nil {
		t.Fatalf("Status = %v, AudioRef = %v, want a ready track holding a file", snapshot.Status, snapshot.AudioRef)
	}
	if snapshot.StoredDurationSeconds == nil || *snapshot.StoredDurationSeconds != 112 {
		t.Errorf("StoredDurationSeconds = %v", snapshot.StoredDurationSeconds)
	}
	if snapshot.CatalogDurationSeconds != nil {
		t.Errorf("CatalogDurationSeconds = %v, the stored length is not a catalog length", snapshot.CatalogDurationSeconds)
	}
	if snapshot.AudioSourceURL == nil || *snapshot.AudioSourceURL != "https://www.youtube.com/watch?v=TB0z2HVFxEE" {
		t.Errorf("AudioSourceURL = %v", snapshot.AudioSourceURL)
	}
}

func TestNewSeededLedger_SavedReadsBackWhatCompleteWrote(t *testing.T) {
	ledger, _ := seededSnapshot(t, SeededTrack{Title: "Song", Artist: "Artist"})
	evidence := json.RawMessage(`{"isrc_source":"fingerprint"}`)

	err := ledger.Complete(context.Background(), ledger.TrackID(), ledger.UserID(), ports.Completion{
		AudioRef: "ref", Duration: 237, ISRC: "CAA509814003",
		Provenance: shared.AcquisitionProvenance("verified"), RejectedSourceKeys: []string{"youtube:abc"},
		Confidence: 0.9, Evidence: evidence,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	saved := ledger.Saved()
	if saved.ISRC != "CAA509814003" || saved.Provenance != "verified" || saved.DurationSeconds != 237 {
		t.Errorf("saved = %+v", saved)
	}
	if !slices.Equal(saved.RejectedSourceKeys, []string{"youtube:abc"}) || string(saved.Evidence) != string(evidence) {
		t.Errorf("saved = %+v", saved)
	}
}

func TestNewSeededLedger_RefusesATrackTheCatalogWouldNotHold(t *testing.T) {
	if _, err := NewSeededLedger(SeededTrack{Artist: "Artist"}); err == nil {
		t.Fatal("NewSeededLedger accepted a track with no title")
	}
}
