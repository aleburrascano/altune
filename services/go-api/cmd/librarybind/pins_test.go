package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	pinTrackID  = "0b8f4c1e-2d3a-4b5c-8d6e-7f8091a2b3c4"
	pinMBID     = "5c1d9d0e-7a3b-4f60-9e11-2b3c4d5e6f70"
	pinNote     = "live take, only Deezer has it"
	pinEvidence = `{"url":"https://example.test/track/1","length_seconds":212}`
)

func pinJSON(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	fields := map[string]any{
		"track_id": pinTrackID, "track_version": 4, "status": "bound", "isrc": "USPR18000001",
		"note": pinNote, "evidence": json.RawMessage(pinEvidence),
	}
	change(fields)
	raw, err := json.Marshal([]any{fields})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParsePins_GuardsEachPin(t *testing.T) {
	cases := []struct {
		name       string
		change     func(map[string]any)
		wantReject bool
		wantErr    error
	}{
		{"a bound pin with an isrc", func(map[string]any) {}, false, nil},
		{"a bound pin with only an mb recording id", func(f map[string]any) { delete(f, "isrc"); f["mb_recording_id"] = pinMBID }, false, nil},
		{"an unofficial pin needs no ids", func(f map[string]any) { delete(f, "isrc"); f["status"] = "unofficial" }, false, nil},
		{"version zero is a real version", func(f map[string]any) { f["track_version"] = 0 }, false, nil},
		{"a bound pin without an isrc or mb id", func(f map[string]any) { delete(f, "isrc"); f["deezer_track_id"] = "77" }, true, errBoundNeedsAnchor},
		{"a missing note", func(f map[string]any) { delete(f, "note") }, true, errMissingNote},
		{"a blank note", func(f map[string]any) { f["note"] = "  " }, true, errMissingNote},
		{"a missing track version", func(f map[string]any) { delete(f, "track_version") }, true, errMissingVersion},
		{"an unbound status", func(f map[string]any) { f["status"] = "unbound" }, true, errBadStatus},
		{"an isrc of the wrong length", func(f map[string]any) { f["isrc"] = "USPR1800001" }, true, errBadISRC},
		{"an isrc with a symbol", func(f map[string]any) { f["isrc"] = "USPR1800000!" }, true, errBadISRC},
		{"an mb recording id that is not a uuid", func(f map[string]any) { f["mb_recording_id"] = "nope" }, true, errBadMBRecordingID},
		{"an unknown field", func(f map[string]any) { f["isrcs"] = "USPR18000001" }, true, nil},
		{"a track id that is not a uuid", func(f map[string]any) { f["track_id"] = "track-1" }, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pins, rejected, err := parsePins(pinJSON(t, tc.change))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantReject != (len(rejected) == 1 && len(pins) == 0) || (!tc.wantReject && len(pins) != 1) {
				t.Fatalf("pins = %+v, rejected = %+v", pins, rejected)
			}
			if tc.wantErr != nil && !errors.Is(rejected[0].Err, tc.wantErr) {
				t.Errorf("rejected = %v, want %v", rejected[0].Err, tc.wantErr)
			}
		})
	}
}

func TestParsePins_CanonicalisesTheIdentifiersAndKeepsTheEvidence(t *testing.T) {
	raw := pinJSON(t, func(f map[string]any) {
		f["isrc"] = " us-pr1 8000001 "
		f["mb_recording_id"] = strings.ToUpper(pinMBID)
	})
	pins, _, err := parsePins(raw)
	if err != nil || len(pins) != 1 {
		t.Fatalf("pins = %+v, %v", pins, err)
	}
	if pins[0].ISRC != "USPR18000001" || pins[0].MBRecordingID != pinMBID || string(pins[0].Evidence) != pinEvidence {
		t.Errorf("pin = %+v", pins[0])
	}
}

func TestParsePins_OneBadPinDoesNotStopTheOthers(t *testing.T) {
	raw := []byte(`[{"track_id":"` + pinTrackID + `","track_version":4,"status":"unofficial","note":"n"}, {"nope":1}, 7]`)
	pins, rejected, err := parsePins(raw)
	if err != nil || len(pins) != 1 || len(rejected) != 2 || rejected[0].Index != 1 || rejected[1].Index != 2 {
		t.Fatalf("pins = %+v, rejected = %+v, err = %v", pins, rejected, err)
	}
}

func TestParsePins_RejectsEveryPinOfADuplicatedTrack(t *testing.T) {
	one := `{"track_id":"` + pinTrackID + `","track_version":4,"status":"unofficial","note":"n"}`
	pins, rejected, err := parsePins([]byte(`[` + one + `,` + one + `]`))
	if err != nil || len(pins) != 0 || len(rejected) != 2 || !errors.Is(rejected[0].Err, errDuplicateTrack) {
		t.Fatalf("pins = %+v, rejected = %+v, err = %v", pins, rejected, err)
	}
}

func TestParsePins_AFileThatIsNotAListIsAnError(t *testing.T) {
	if _, _, err := parsePins([]byte(`{"track_id":"x"}`)); !errors.Is(err, errPinsNotAList) {
		t.Fatalf("err = %v", err)
	}
}

func pinnedTrack() libraryTrack {
	return with2(celebration(), func(tr *libraryTrack) { tr.ID = pinTrackID })
}

func TestDecidePin_FillsOnlyUnboundOrMissingRowsOfAnUnchangedTrack(t *testing.T) {
	p := pin{TrackID: pinTrackID, TrackVersion: 4, Status: statusBound, ISRC: celebrationISRC, Note: pinNote}
	moved := with2(pinnedTrack(), func(tr *libraryTrack) { tr.Version = 5 })
	cases := []struct {
		name    string
		targets map[string]pinTarget
		want    pinSkip
	}{
		{"a track with no identity row", map[string]pinTarget{pinTrackID: {Track: pinnedTrack()}}, pinWrites},
		{"a track with an unbound row", map[string]pinTarget{pinTrackID: {Track: pinnedTrack(), IdentityStatus: statusUnbound}}, pinWrites},
		{"a track with a bound row", map[string]pinTarget{pinTrackID: {Track: pinnedTrack(), IdentityStatus: statusBound}}, pinSkipAlreadyDecided},
		{"a track with an unofficial row", map[string]pinTarget{pinTrackID: {Track: pinnedTrack(), IdentityStatus: statusUnofficial}}, pinSkipAlreadyDecided},
		{"a track whose version moved on", map[string]pinTarget{pinTrackID: {Track: moved}}, pinSkipTrackChanged},
		{"a track that is not in the library", map[string]pinTarget{}, pinSkipUnknownTrack},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decidePin(p, tc.targets).Skip; got != tc.want {
				t.Fatalf("skip = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPinIdentity_CarriesTheNoteTheIdsAndTheEvidence(t *testing.T) {
	p := pin{
		TrackID: pinTrackID, TrackVersion: 4, Status: statusBound, ISRC: celebrationISRC, MBRecordingID: pinMBID,
		DeezerTrackID: "77", AppleSongID: "88", Note: pinNote, Evidence: json.RawMessage(pinEvidence),
	}
	got := p.identity(pinnedTrack())
	proof, err := json.Marshal(got.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != statusBound || got.Reason != "pinned: "+pinNote || got.ISRC != celebrationISRC || got.MBRecordingID != pinMBID ||
		got.DeezerTrackID != "77" || got.AppleSongID != "88" {
		t.Errorf("identity = %+v", got)
	}
	if !strings.Contains(string(proof), `"pin":`+pinEvidence) || !strings.Contains(string(proof), `"title":"Celebration"`) {
		t.Errorf("evidence = %s", proof)
	}
}

type pinnedSink struct {
	saved   map[string]identity
	verdict saveVerdict
}

func (s *pinnedSink) save(_ context.Context, _ uuid.UUID, tr libraryTrack, found identity) (saveVerdict, error) {
	s.saved[tr.ID] = found
	return s.verdict, nil
}

func newReport() *strings.Builder { return &strings.Builder{} }

func newPinnedSink(verdict saveVerdict) *pinnedSink {
	return &pinnedSink{saved: map[string]identity{}, verdict: verdict}
}

func loaderOf(targets map[string]pinTarget) pinTargetLoader {
	return func(context.Context, []string) (map[string]pinTarget, error) { return targets, nil }
}

func TestRunPins_DryRunWritesNothingAndApplyWritesTheFillablePins(t *testing.T) {
	targets := map[string]pinTarget{pinTrackID: {Track: pinnedTrack(), IdentityStatus: statusUnbound}}
	raw := pinJSON(t, func(map[string]any) {})

	var dry bytes.Buffer
	if err := runPins(t.Context(), raw, loaderOf(targets), nil, uuid.New(), &dry); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dry.String(), "dry run, nothing written") || !strings.Contains(dry.String(), "pinned: "+pinNote) {
		t.Errorf("dry report = %s", dry.String())
	}

	sink := newPinnedSink(saveWritten)
	var applied bytes.Buffer
	if err := runPins(t.Context(), raw, loaderOf(targets), sink, uuid.New(), &applied); err != nil {
		t.Fatal(err)
	}
	if got := sink.saved[pinTrackID]; got.Status != statusBound || got.Reason != "pinned: "+pinNote || len(sink.saved) != 1 {
		t.Errorf("saved = %+v", sink.saved)
	}
	if !strings.Contains(applied.String(), "pinned: 1, skipped: 0, rejected: 0") {
		t.Errorf("applied report = %s", applied.String())
	}
}

func TestRunPins_ReportsAStaleTrackAndWritesNothingForIt(t *testing.T) {
	moved := with2(pinnedTrack(), func(tr *libraryTrack) { tr.Version = 9 })
	sink := newPinnedSink(saveWritten)
	var out bytes.Buffer
	err := runPins(t.Context(), pinJSON(t, func(map[string]any) {}), loaderOf(map[string]pinTarget{pinTrackID: {Track: moved}}), sink, uuid.New(), &out)
	if err != nil || len(sink.saved) != 0 {
		t.Fatalf("saved = %+v, err = %v", sink.saved, err)
	}
	if !strings.Contains(out.String(), "pinned against version 4, track is now 9") {
		t.Errorf("report = %s", out.String())
	}
}

func TestRunPins_ASaveThatLostARaceIsReportedAsSkipped(t *testing.T) {
	sink := newPinnedSink(saveSkipped)
	var out bytes.Buffer
	err := runPins(t.Context(), pinJSON(t, func(map[string]any) {}), loaderOf(map[string]pinTarget{pinTrackID: {Track: pinnedTrack()}}), sink, uuid.New(), &out)
	if err != nil || !strings.Contains(out.String(), "pinned: 0, skipped: 1, rejected: 0") {
		t.Fatalf("err = %v, report = %s", err, out.String())
	}
}

func TestRunPins_ARejectedPinFailsTheRunButTheValidOnesStillWrite(t *testing.T) {
	good := `{"track_id":"` + pinTrackID + `","track_version":4,"status":"unofficial","note":"n"}`
	raw := []byte(`[` + good + `,{"track_id":"` + uuid.NewString() + `","track_version":1,"status":"unofficial"}]`)
	sink := newPinnedSink(saveWritten)
	var out bytes.Buffer
	err := runPins(t.Context(), raw, loaderOf(map[string]pinTarget{pinTrackID: {Track: pinnedTrack()}}), sink, uuid.New(), &out)
	if !errors.Is(err, errPinsRejected) || len(sink.saved) != 1 {
		t.Fatalf("err = %v, saved = %+v", err, sink.saved)
	}
	if !strings.Contains(out.String(), "note is required") {
		t.Errorf("report = %s", out.String())
	}
}

func TestParsePins_AReplacementNeedsAnAnchorAndEvidence(t *testing.T) {
	replacesISRC := func(f map[string]any) { f["replaces"] = map[string]any{"isrc": "USPR18000009"} }
	cases := []struct {
		name    string
		change  func(map[string]any)
		wantErr error
	}{
		{"replaces by isrc", replacesISRC, nil},
		{"replaces by mb recording id", func(f map[string]any) { f["replaces"] = map[string]any{"mb_recording_id": pinMBID} }, nil},
		{"replaces with no ids", func(f map[string]any) { f["replaces"] = map[string]any{} }, errReplacesNoAnchor},
		{"replaces with a bad isrc", func(f map[string]any) { f["replaces"] = map[string]any{"isrc": "nope"} }, errBadISRC},
		{"replaces with a bad mb id", func(f map[string]any) { f["replaces"] = map[string]any{"mb_recording_id": "nope"} }, errBadMBRecordingID},
		{"replaces without evidence", func(f map[string]any) { replacesISRC(f); delete(f, "evidence") }, errMissingEvidence},
		{"replaces with null evidence", func(f map[string]any) { replacesISRC(f); f["evidence"] = nil }, errMissingEvidence},
		{"replaces without a note", func(f map[string]any) { replacesISRC(f); delete(f, "note") }, errMissingNote},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pins, rejected, err := parsePins(pinJSON(t, tc.change))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantErr == nil {
				if len(pins) != 1 || len(rejected) != 0 {
					t.Fatalf("pins %d, rejected %+v", len(pins), rejected)
				}
				return
			}
			if len(pins) != 0 || len(rejected) != 1 {
				t.Fatalf("pins %d, rejected %+v", len(pins), rejected)
			}
			if !errors.Is(rejected[0].Err, tc.wantErr) {
				t.Errorf("err = %v, want %v", rejected[0].Err, tc.wantErr)
			}
		})
	}
}

func TestDecidePin_AReplacementWritesOnlyOverABoundRowThatMatches(t *testing.T) {
	replacing := func(r replaces) pin {
		return pin{TrackID: pinTrackID, TrackVersion: 4, Status: statusBound, Replaces: &r}
	}
	boundRow := pinTarget{Track: pinnedTrack(), IdentityStatus: statusBound, IdentityISRC: "USPR18000009", IdentityMBRecordingID: pinMBID}
	cases := []struct {
		name   string
		pin    pin
		target pinTarget
		want   pinSkip
	}{
		{"isrc matches", replacing(replaces{ISRC: "USPR18000009"}), boundRow, pinWrites},
		{"both ids match", replacing(replaces{ISRC: "USPR18000009", MBRecordingID: pinMBID}), boundRow, pinWrites},
		{"isrc differs", replacing(replaces{ISRC: "USPR18000001"}), boundRow, pinSkipReplaceMismatch},
		{"one of two ids differs", replacing(replaces{ISRC: "USPR18000009", MBRecordingID: "other"}), boundRow, pinSkipReplaceMismatch},
		{"an unbound row", replacing(replaces{ISRC: "USPR18000009"}), pinTarget{Track: pinnedTrack(), IdentityStatus: statusUnbound}, pinSkipNothingToReplace},
		{"no row", replacing(replaces{ISRC: "USPR18000009"}), pinTarget{Track: pinnedTrack()}, pinSkipNothingToReplace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decidePin(tc.pin, map[string]pinTarget{pinTrackID: tc.target}).Skip; got != tc.want {
				t.Errorf("skip = %q, want %q", got, tc.want)
			}
		})
	}
}
