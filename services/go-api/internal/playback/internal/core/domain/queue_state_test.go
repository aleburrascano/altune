package domain_test

import (
	"altune/go-api/internal/playback"
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewQueueState_Validation(t *testing.T) {
	tests := []struct {
		name       string
		trackIds   []string
		currentIdx int
		positionMs int64
		wantErr    bool
		wantIdx    int
	}{
		{name: "valid in-range", trackIds: []string{canonicalTrackA, canonicalTrackB, canonicalTrackA}, currentIdx: 1, positionMs: 5000, wantIdx: 1},
		{name: "empty queue normalizes idx to 0", trackIds: []string{}, currentIdx: -1, positionMs: 0, wantIdx: 0},
		{name: "idx past end rejected", trackIds: []string{"a", "b"}, currentIdx: 2, positionMs: 0, wantErr: true},
		{name: "negative idx rejected", trackIds: []string{"a"}, currentIdx: -1, positionMs: 0, wantErr: true},
		{name: "negative position rejected", trackIds: []string{"a"}, currentIdx: 0, positionMs: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := playback.NewQueueState(playback.QueueStateInput{
				UserId:     testUser(),
				TrackIds:   tt.trackIds,
				CurrentIdx: tt.currentIdx,
				PositionMs: tt.positionMs,
			}, time.Now())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.CurrentIdx != tt.wantIdx {
				t.Errorf("CurrentIdx = %d, want %d", state.CurrentIdx, tt.wantIdx)
			}
		})
	}
}

func TestResume_WithNothingStoredIsAnEmptyQueue(t *testing.T) {
	state, err := playback.MustNewQueueService(&storeSpy{}, nil, time.Now).Resume(context.Background(), testUser())
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(state.TrackIds) != 0 {
		t.Errorf("TrackIds = %v, want empty", state.TrackIds)
	}
	if state.CurrentIdx != 0 {
		t.Errorf("CurrentIdx = %d, want 0", state.CurrentIdx)
	}
	if state.RepeatMode != playback.RepeatOff {
		t.Errorf("RepeatMode = %v, want RepeatOff", state.RepeatMode)
	}
}

func TestQueueState_CurrentTrackId(t *testing.T) {
	tests := []struct {
		name      string
		trackIds  []string
		idx       int
		wantId    string
		wantFound bool
	}{
		{name: "first track", trackIds: []string{"a", "b"}, idx: 0, wantId: "a", wantFound: true},
		{name: "last track", trackIds: []string{"a", "b"}, idx: 1, wantId: "b", wantFound: true},
		{name: "empty queue", trackIds: []string{}, idx: 0},
		{name: "nil queue", trackIds: nil, idx: 0},
		{name: "bypassed: empty queue non-zero idx", trackIds: []string{}, idx: 2},
		{name: "bypassed: idx past end", trackIds: []string{"a"}, idx: 1},
		{name: "bypassed: negative idx", trackIds: []string{"a"}, idx: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &playback.QueueState{TrackIds: tt.trackIds, CurrentIdx: tt.idx}
			gotId, gotFound := state.CurrentTrackId()
			if gotId != tt.wantId || gotFound != tt.wantFound {
				t.Errorf("CurrentTrackId() = (%q, %v), want (%q, %v)", gotId, gotFound, tt.wantId, tt.wantFound)
			}
		})
	}
}

func TestQueueState_CurrentTrackId_ConstructedEmptyQueueHasNone(t *testing.T) {
	state, err := playback.NewQueueState(playback.QueueStateInput{UserId: testUser(), CurrentIdx: 5}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id, ok := state.CurrentTrackId(); ok {
		t.Errorf("empty queue reported current track %q", id)
	}
}

func repeatIds(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = canonicalTrackA
	}
	return ids
}

func TestNewQueueState_BoundsQueueLength(t *testing.T) {
	tests := []struct {
		name         string
		trackIds     []string
		naturalOrder []string
		wantErr      bool
	}{
		{name: "trackIds at limit accepted", trackIds: repeatIds(maxQueueLength)},
		{name: "trackIds over limit rejected", trackIds: repeatIds(maxQueueLength + 1), wantErr: true},
		{name: "naturalOrder at limit accepted", trackIds: repeatIds(maxQueueLength), naturalOrder: repeatIds(maxQueueLength)},
		{name: "naturalOrder over limit rejected", trackIds: repeatIds(1), naturalOrder: repeatIds(maxQueueLength + 1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := playback.NewQueueState(playback.QueueStateInput{
				UserId:       testUser(),
				TrackIds:     tt.trackIds,
				NaturalOrder: tt.naturalOrder,
			}, time.Now())
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected over-limit queue to be rejected")
			}
			validationCode(t, err)
		})
	}
}

func TestNewQueueState_BoundsStringLength(t *testing.T) {
	huge := strings.Repeat("a", 500*1024)
	tests := []struct {
		name  string
		input playback.QueueStateInput
	}{
		{
			name:  "oversized trackIds element rejected",
			input: playback.QueueStateInput{UserId: testUser(), TrackIds: []string{huge}},
		},
		{
			name:  "oversized naturalOrder element rejected",
			input: playback.QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, NaturalOrder: []string{huge}},
		},
		{
			name:  "oversized sourceId rejected",
			input: playback.QueueStateInput{UserId: testUser(), SourceId: huge},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := playback.NewQueueState(tt.input, time.Now())
			if err == nil {
				t.Fatal("expected oversized queue string to be rejected")
			}
			validationCode(t, err)
		})
	}
}

func TestNewQueueState_AcceptsStringAtMaxLength(t *testing.T) {
	atLimit := "search:" + strings.Repeat("a", maxQueueStringBytes-len("search:"))
	_, err := playback.NewQueueState(playback.QueueStateInput{
		UserId:   testUser(),
		SourceId: atLimit,
	}, time.Now())
	if err != nil {
		t.Fatalf("string at max length should be accepted: %v", err)
	}
}

func TestNewQueueState_RejectsNulBytes(t *testing.T) {
	tests := []struct {
		name  string
		input playback.QueueStateInput
	}{
		{
			name:  "nul in trackIds",
			input: playback.QueueStateInput{UserId: testUser(), TrackIds: []string{"a\x00b"}, CurrentIdx: 0},
		},
		{
			name:  "nul in naturalOrder",
			input: playback.QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, NaturalOrder: []string{"x\x00y"}},
		},
		{
			name:  "nul in sourceId",
			input: playback.QueueStateInput{UserId: testUser(), SourceId: "playlist:pid:na\x00me"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := playback.NewQueueState(tt.input, time.Now())
			if err == nil {
				t.Fatal("expected NUL byte in string element to be rejected")
			}
			validationCode(t, err)
		})
	}
}

func stateFromInput(in playback.QueueStateInput) *playback.QueueState {
	return &playback.QueueState{
		UserId:       in.UserId,
		TrackIds:     in.TrackIds,
		CurrentIdx:   in.CurrentIdx,
		PositionMs:   in.PositionMs,
		Shuffled:     in.Shuffled,
		RepeatMode:   in.RepeatMode,
		SourceId:     in.SourceId,
		NaturalOrder: in.NaturalOrder,
	}
}

func TestQueueInvariants_ErrorNamesTheOffendingField(t *testing.T) {
	tests := []struct {
		name    string
		input   playback.QueueStateInput
		wantMsg string
	}{
		{
			name:    "negative positionMs",
			input:   playback.QueueStateInput{TrackIds: []string{"a"}, PositionMs: -1},
			wantMsg: "positionMs must be non-negative",
		},
		{
			name:    "trackIds over limit",
			input:   playback.QueueStateInput{TrackIds: repeatIds(maxQueueLength + 1)},
			wantMsg: "trackIds length",
		},
		{
			name:    "naturalOrder over limit",
			input:   playback.QueueStateInput{TrackIds: []string{"a"}, NaturalOrder: repeatIds(maxQueueLength + 1)},
			wantMsg: "naturalOrder length",
		},
		{
			name:    "nul in naturalOrder element",
			input:   playback.QueueStateInput{TrackIds: []string{"a"}, NaturalOrder: []string{"x\x00y"}},
			wantMsg: "naturalOrder contains a NUL byte",
		},
		{
			name:    "nul in sourceId",
			input:   playback.QueueStateInput{TrackIds: []string{"a"}, SourceId: "playlist:pid:na\x00me"},
			wantMsg: "sourceId contains a NUL byte",
		},
		{
			name:    "currentIdx past the end of trackIds",
			input:   playback.QueueStateInput{TrackIds: []string{"a", "b"}, CurrentIdx: 5},
			wantMsg: "currentIdx 5 out of range [0, 2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			in.UserId = testUser()
			_, constructErr := playback.NewQueueState(in, time.Now())
			validateErr := stateFromInput(in).Validate()

			for _, site := range []struct {
				name string
				err  error
			}{
				{name: "NewQueueState", err: constructErr},
				{name: "(*QueueState).Validate", err: validateErr},
			} {
				if site.err == nil {
					t.Fatalf("%s: expected an error, got nil", site.name)
				}
				if !strings.Contains(site.err.Error(), tt.wantMsg) {
					t.Errorf("%s: error = %q, want it to name %q", site.name, site.err.Error(), tt.wantMsg)
				}
			}
		})
	}
}

func TestNewQueueState_RejectsEmptyCurrentTrackId(t *testing.T) {
	_, err := playback.NewQueueState(playback.QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", ""},
		CurrentIdx: 1,
	}, time.Now())
	if err == nil {
		t.Fatal("expected an empty current-track id to be rejected")
	}
	validationCode(t, err)
}

func TestQueueState_Validate_RejectsEmptyCurrentTrackId(t *testing.T) {
	state := &playback.QueueState{UserId: testUser(), TrackIds: []string{""}, CurrentIdx: 0}

	if err := state.Validate(); err == nil {
		t.Fatal("expected the pre-write check to reject an empty current-track id")
	}
}

func TestNewQueueState_AcceptsRealCurrentTrackId(t *testing.T) {
	state, err := playback.NewQueueState(playback.QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{canonicalTrackA, canonicalTrackB},
		CurrentIdx: 1,
	}, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id, ok := state.CurrentTrackId(); !ok || id != canonicalTrackB {
		t.Errorf("CurrentTrackId() = (%q, %v), want (%q, true)", id, ok, canonicalTrackB)
	}
}

func TestQueueStateValidation_EachCauseHasItsOwnCode(t *testing.T) {
	oversized := strings.Repeat("a", maxQueueStringBytes+1)
	tests := []struct {
		name     string
		reject   func() error
		wantCode string
	}{
		{
			name:     "negative positionMs",
			reject:   rejectedState(playback.QueueStateInput{TrackIds: []string{"a"}, PositionMs: -1}),
			wantCode: "playback.position_ms_negative",
		},
		{
			name:     "trackIds over the length limit",
			reject:   rejectedState(playback.QueueStateInput{TrackIds: repeatIds(maxQueueLength + 1)}),
			wantCode: "playback.queue_too_long",
		},
		{
			name:     "oversized trackIds element",
			reject:   rejectedState(playback.QueueStateInput{TrackIds: []string{oversized}}),
			wantCode: "playback.string_too_long",
		},
		{
			name:     "NUL byte in sourceId",
			reject:   rejectedState(playback.QueueStateInput{TrackIds: []string{"a"}, SourceId: "playlist:pid:na\x00me"}),
			wantCode: "playback.string_contains_nul",
		},
		{
			name:     "currentIdx past the end of trackIds",
			reject:   rejectedState(playback.QueueStateInput{TrackIds: []string{"a", "b"}, CurrentIdx: 5}),
			wantCode: "playback.current_idx_out_of_range",
		},
		{
			name:     "empty id at currentIdx",
			reject:   rejectedState(playback.QueueStateInput{TrackIds: []string{"a", ""}, CurrentIdx: 1}),
			wantCode: "playback.current_track_id_missing",
		},
		{
			name:     "unknown repeat mode",
			reject:   func() error { _, err := saveQueue(playback.SaveQueueStateInput{RepeatMode: "sometimes"}); return err },
			wantCode: "playback.unknown_repeat_mode",
		},
	}

	codeOfCause := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validationCode(t, tt.reject())

			if got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
			codeOfCause[tt.name] = got
		})
	}
	assertOneCodePerCause(t, codeOfCause)
}

func assertOneCodePerCause(t *testing.T, codeOfCause map[string]string) {
	t.Helper()
	causeOfCode := map[string]string{}
	for cause, code := range codeOfCause {
		if other, taken := causeOfCode[code]; taken {
			t.Errorf("%q and %q both return %q, so a client cannot tell them apart", cause, other, code)
		}
		causeOfCode[code] = cause
	}
}

func rejectedState(in playback.QueueStateInput) func() error {
	return func() error {
		in.UserId = testUser()
		_, err := playback.NewQueueState(in, time.Now())
		return err
	}
}

func TestQueueStateValidation_IsAnInvalidInputWithItsOwnCode(t *testing.T) {
	_, err := playback.NewQueueState(playback.QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, PositionMs: -1}, time.Now())

	if got := validationCode(t, err); got != "playback.position_ms_negative" {
		t.Fatalf("code = %q, want playback.position_ms_negative", got)
	}
}

func TestNewQueueState_StoresTrackIdsInCanonicalForm(t *testing.T) {
	state, err := playback.NewQueueState(playback.QueueStateInput{
		UserId:       testUser(),
		TrackIds:     []string{"3F2504E0-4F89-11D3-9A0C-0305E82C3301", "{6ba7b810-9dad-11d1-80b4-00c04fd430c8}"},
		NaturalOrder: []string{"urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8", "3f2504e04f8911d39a0c0305e82c3301"},
		Shuffled:     true,
	}, time.Now())
	if err != nil {
		t.Fatalf("NewQueueState: %v", err)
	}

	if got := strings.Join(state.TrackIds, ","); got != canonicalTrackA+","+canonicalTrackB {
		t.Errorf("TrackIds = %s, want the canonical lowercase form", got)
	}
	if got := strings.Join(state.NaturalOrder, ","); got != canonicalTrackB+","+canonicalTrackA {
		t.Errorf("NaturalOrder = %s, want the canonical lowercase form", got)
	}
}

func TestNewQueueState_RejectsGarbageTrackIds(t *testing.T) {
	tests := []struct {
		name     string
		input    playback.QueueStateInput
		wantCode string
	}{
		{
			name:     "unparseable track id",
			input:    playback.QueueStateInput{TrackIds: []string{"not-a-uuid"}},
			wantCode: "playback.malformed_track_id",
		},
		{
			name:     "empty element away from currentIdx",
			input:    playback.QueueStateInput{TrackIds: []string{canonicalTrackA, ""}},
			wantCode: "playback.malformed_track_id",
		},
		{
			name:     "unparseable naturalOrder element",
			input:    playback.QueueStateInput{TrackIds: []string{canonicalTrackA}, NaturalOrder: []string{"x"}},
			wantCode: "playback.malformed_track_id",
		},
		{
			name:     "naturalOrder holding another track",
			input:    playback.QueueStateInput{TrackIds: []string{canonicalTrackA}, NaturalOrder: []string{canonicalTrackB}},
			wantCode: "playback.natural_order_mismatch",
		},
		{
			name:     "naturalOrder missing a repeated track",
			input:    playback.QueueStateInput{TrackIds: []string{canonicalTrackA, canonicalTrackA}, NaturalOrder: []string{canonicalTrackA}},
			wantCode: "playback.natural_order_mismatch",
		},
		{
			name:     "shuffled queue without a natural order",
			input:    playback.QueueStateInput{TrackIds: []string{canonicalTrackA}, Shuffled: true},
			wantCode: "playback.natural_order_mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validationCode(t, rejectedState(tt.input)())

			if got != tt.wantCode {
				t.Errorf("code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestNewQueueState_UnshuffledQueueMayOmitNaturalOrder(t *testing.T) {
	state, err := playback.NewQueueState(playback.QueueStateInput{UserId: testUser(), TrackIds: []string{canonicalTrackA, canonicalTrackB}}, time.Now())
	if err != nil {
		t.Fatalf("an unshuffled queue with no natural order must still save: %v", err)
	}
	if len(state.NaturalOrder) != 0 {
		t.Errorf("NaturalOrder = %v, want it left empty", state.NaturalOrder)
	}
}
