package main

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	catalogPorts "altune/go-api/internal/catalog/ports"
)

type fakeLookups struct {
	byISRC    map[string][]string
	canonical map[string]string
	err       error
	calls     int
}

func (f *fakeLookups) RecordingsByISRC(_ context.Context, isrc string) ([]string, error) {
	f.calls++
	return f.byISRC[isrc], f.err
}

func (f *fakeLookups) CanonicalRecording(_ context.Context, id string) (string, error) {
	f.calls++
	return f.canonical[id], f.err
}

type memoryStream struct{ *bytes.Reader }

func (memoryStream) Close() error { return nil }

type memoryStore struct {
	catalogPorts.AudioStore
	files map[string][]byte
}

func (s memoryStore) Stream(_ context.Context, ref string) (catalogPorts.AudioStream, int64, error) {
	data, ok := s.files[ref]
	if !ok {
		return nil, 0, errors.New("no such object")
	}
	return memoryStream{bytes.NewReader(data)}, int64(len(data)), nil
}

const (
	fpcalcPrintsFingerprint = "#!/bin/sh\nprintf '{\"duration\": 200, \"fingerprint\": \"AQAAdkmVJUqSpNce\"}'\n"
	fpcalcFails             = "#!/bin/sh\necho cannot decode >&2\nexit 1\n"
)

func writeFakeFpcalc(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fpcalc"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake fpcalc: %v", err)
	}
	return dir
}

func newVerifier(t *testing.T, payloadFile string, lookups recordingLookups) *verifier {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", payloadFile))
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	identifier := chromaprint.NewIdentifier(writeFakeFpcalc(t, fpcalcPrintsFingerprint), "key").WithEndpoint(server.URL).WithRequestsPerSecond(1000)
	return &verifier{
		store:    memoryStore{files: map[string][]byte{"u/song.mp3": []byte("audio")}},
		identify: identifier,
		lookups:  lookups,
		minScore: chromaprint.MinScore,
		progress: io.Discard,
	}
}

func trackWith(identity boundIdentity) libraryTrack {
	return libraryTrack{ID: "t-1", UserID: "u-1", Artist: "Artist", Title: "Song", AudioRef: "u/song.mp3", Identity: identity}
}

func TestVerify_VerdictFromRecordedAcoustIDPayloads(t *testing.T) {
	bound := boundIdentity{Status: statusBound, MBRecordingID: "rec-bound", ISRC: "USAAA0000001"}
	isrcOnly := boundIdentity{Status: statusBound, ISRC: "USAAA0000001"}
	tests := []struct {
		name         string
		payload      string
		identity     boundIdentity
		lookups      *fakeLookups
		wantVerdict  verdict
		wantBasis    string
		wantTopID    string
		wantLookups  int
		wantErrorMsg string
	}{
		{name: "bound recording present above the threshold", payload: "acoustid_bound_recording.json", identity: bound, lookups: &fakeLookups{}, wantVerdict: verdictConfirmed, wantBasis: basisRecordingID, wantTopID: "rec-bound"},
		{name: "isrc-only row confirmed by a shared recording", payload: "acoustid_isrc_sibling.json", identity: isrcOnly, lookups: &fakeLookups{byISRC: map[string][]string{"USAAA0000001": {"rec-sibling"}}}, wantVerdict: verdictConfirmed, wantBasis: basisISRC, wantTopID: "rec-sibling", wantLookups: 1},
		{name: "bound recording merged into the one the audio returns", payload: "acoustid_isrc_sibling.json", identity: boundIdentity{Status: statusBound, MBRecordingID: "rec-old"}, lookups: &fakeLookups{canonical: map[string]string{"rec-old": "rec-sibling"}}, wantVerdict: verdictConfirmed, wantBasis: basisMergedID, wantTopID: "rec-sibling", wantLookups: 1},
		{name: "confident recordings none of which is the bound one", payload: "acoustid_other_recording.json", identity: bound, lookups: &fakeLookups{canonical: map[string]string{"rec-bound": "rec-bound"}, byISRC: map[string][]string{"USAAA0000001": {"rec-bound"}}}, wantVerdict: verdictMismatch, wantTopID: "rec-other", wantLookups: 2},
		{name: "low score is unknown", payload: "acoustid_low_score.json", identity: bound, lookups: &fakeLookups{}, wantVerdict: verdictUnknown},
		{name: "no fingerprint match is unknown", payload: "acoustid_no_results.json", identity: bound, lookups: &fakeLookups{}, wantVerdict: verdictUnknown},
		{name: "match linked to no recording is unknown", payload: "acoustid_unlinked.json", identity: bound, lookups: &fakeLookups{}, wantVerdict: verdictUnknown},
		{name: "unofficial track reports what AcoustID says", payload: "acoustid_bound_recording.json", identity: boundIdentity{Status: statusUnofficial, Reason: "no catalog hit"}, lookups: &fakeLookups{}, wantVerdict: verdictUnofficial, wantTopID: "rec-bound"},
		{name: "track with no identity row is unbound", payload: "acoustid_bound_recording.json", identity: boundIdentity{Status: statusNone}, lookups: &fakeLookups{}, wantVerdict: verdictUnbound, wantTopID: "rec-bound"},
		{name: "bound by streaming ids only cannot be compared", payload: "acoustid_bound_recording.json", identity: boundIdentity{Status: statusBound, DeezerTrackID: "1"}, lookups: &fakeLookups{}, wantVerdict: verdictUnverifiable, wantTopID: "rec-bound"},
		{name: "a failed musicbrainz lookup is never a mismatch", payload: "acoustid_other_recording.json", identity: bound, lookups: &fakeLookups{err: errors.New("musicbrainz down")}, wantVerdict: verdictFailed, wantLookups: 1, wantErrorMsg: "musicbrainz down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVerifier(t, tt.payload, tt.lookups)

			got := v.verify(context.Background(), trackWith(tt.identity))

			if got.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %q (error %q), want %q", got.Verdict, got.Error, tt.wantVerdict)
			}
			if got.Basis != tt.wantBasis {
				t.Errorf("basis = %q, want %q", got.Basis, tt.wantBasis)
			}
			if tt.wantTopID != "" && (len(got.Candidates) == 0 || got.Candidates[0].MBRecordingID != tt.wantTopID) {
				t.Errorf("candidates = %+v, want top recording %q", got.Candidates, tt.wantTopID)
			}
			if tt.lookups.calls != tt.wantLookups {
				t.Errorf("musicbrainz lookups = %d, want %d", tt.lookups.calls, tt.wantLookups)
			}
			if !strings.Contains(got.Error, tt.wantErrorMsg) {
				t.Errorf("error = %q, want it to contain %q", got.Error, tt.wantErrorMsg)
			}
		})
	}
}

func TestVerify_MismatchReportsTopRecordingsWithTitleArtistLengthScore(t *testing.T) {
	v := newVerifier(t, "acoustid_other_recording.json", &fakeLookups{})

	got := v.verify(context.Background(), trackWith(boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"}))

	want := candidate{AcoustID: "ac-2", MBRecordingID: "rec-other", Title: "Different Song", Artists: []string{"Someone Else"}, DurationSeconds: 187, Score: 0.95}
	if got.Verdict != verdictMismatch || len(got.Candidates) != 2 {
		t.Fatalf("verdict = %q with %d candidates, want mismatch with 2", got.Verdict, len(got.Candidates))
	}
	if first := got.Candidates[0]; first.AcoustID != want.AcoustID || first.MBRecordingID != want.MBRecordingID || first.Title != want.Title ||
		first.Artists[0] != want.Artists[0] || first.DurationSeconds != want.DurationSeconds || first.Score != want.Score {
		t.Errorf("top candidate = %+v, want %+v", first, want)
	}
	if got.TopScore != 0.95 {
		t.Errorf("top score = %v, want 0.95", got.TopScore)
	}
}

func TestVerify_RaisedThresholdTurnsALessConfidentMatchIntoUnknown(t *testing.T) {
	v := newVerifier(t, "acoustid_other_recording.json", &fakeLookups{})
	v.minScore = 0.99

	got := v.verify(context.Background(), trackWith(boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"}))

	if got.Verdict != verdictUnknown {
		t.Errorf("verdict = %q, want unknown when no result reaches 0.99", got.Verdict)
	}
}

func TestVerify_MissingAudioFailsTheTrackNotTheRun(t *testing.T) {
	v := newVerifier(t, "acoustid_bound_recording.json", &fakeLookups{})
	missing := trackWith(boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"})
	missing.AudioRef = "u/gone.mp3"
	noRef := trackWith(boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"})
	noRef.AudioRef = ""

	results := v.verifyAll(context.Background(), []libraryTrack{missing, trackWith(boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"}), noRef}, 2)

	got := []verdict{results[0].Verdict, results[1].Verdict, results[2].Verdict}
	want := []verdict{verdictFailed, verdictConfirmed, verdictFailed}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("verdicts = %v, want %v", got, want)
			break
		}
	}
}

func TestVerify_FingerprintFailureFailsTheTrack(t *testing.T) {
	v := newVerifier(t, "acoustid_bound_recording.json", &fakeLookups{})
	v.identify = chromaprint.NewIdentifier(writeFakeFpcalc(t, fpcalcFails), "key")

	got := v.verify(context.Background(), trackWith(boundIdentity{Status: statusBound, MBRecordingID: "rec-bound"}))

	if got.Verdict != verdictFailed || !strings.Contains(got.Error, "fingerprint") {
		t.Errorf("verdict = %q error = %q, want failed with a fingerprint error", got.Verdict, got.Error)
	}
}
