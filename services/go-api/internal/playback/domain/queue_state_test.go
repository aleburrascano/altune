package domain

import (
	"altune/go-api/internal/shared"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestNewQueueState_Validation(t *testing.T) {
	tests := []struct {
		name       string
		trackIds   []string
		currentIdx int
		positionMs int64
		wantErr    bool
		wantIdx    int
	}{
		{name: "valid in-range", trackIds: []string{"a", "b", "c"}, currentIdx: 1, positionMs: 5000, wantIdx: 1},
		{name: "empty queue normalizes idx to 0", trackIds: []string{}, currentIdx: -1, positionMs: 0, wantIdx: 0},
		{name: "idx past end rejected", trackIds: []string{"a", "b"}, currentIdx: 2, positionMs: 0, wantErr: true},
		{name: "negative idx rejected", trackIds: []string{"a"}, currentIdx: -1, positionMs: 0, wantErr: true},
		{name: "negative position rejected", trackIds: []string{"a"}, currentIdx: 0, positionMs: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := NewQueueState(QueueStateInput{
				UserId:     testUser(),
				TrackIds:   tt.trackIds,
				CurrentIdx: tt.currentIdx,
				PositionMs: tt.positionMs,
			})
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

func TestRehydrateQueueState_RejectsStoredRowWithCurrentIdxPastEnd(t *testing.T) {
	_, err := RehydrateQueueState(QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", "b"},
		CurrentIdx: 9,
	}, time.Now())
	if err == nil {
		t.Fatal("expected out-of-range current_idx to be rejected on rehydrate")
	}
}

func TestRehydrateQueueState_PreservesUpdatedAt(t *testing.T) {
	stored := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	state, err := RehydrateQueueState(QueueStateInput{
		UserId:   testUser(),
		TrackIds: []string{"a"},
	}, stored)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.UpdatedAt.Equal(stored) {
		t.Errorf("UpdatedAt = %v, want %v (stored value, not now)", state.UpdatedAt, stored)
	}
}

func TestEmptyQueueState_IsValidAndEmpty(t *testing.T) {
	state := EmptyQueueState(testUser())
	if len(state.TrackIds) != 0 {
		t.Errorf("TrackIds = %v, want empty", state.TrackIds)
	}
	if state.CurrentIdx != 0 {
		t.Errorf("CurrentIdx = %d, want 0", state.CurrentIdx)
	}
	if state.RepeatMode != RepeatOff {
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
			state := &QueueState{TrackIds: tt.trackIds, CurrentIdx: tt.idx}
			gotId, gotFound := state.CurrentTrackId()
			if gotId != tt.wantId || gotFound != tt.wantFound {
				t.Errorf("CurrentTrackId() = (%q, %v), want (%q, %v)", gotId, gotFound, tt.wantId, tt.wantFound)
			}
		})
	}
}

func TestQueueState_CurrentTrackId_ConstructedEmptyQueueHasNone(t *testing.T) {
	state, err := NewQueueState(QueueStateInput{UserId: testUser(), CurrentIdx: 5})
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
		ids[i] = "a"
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
		{name: "trackIds at limit accepted", trackIds: repeatIds(MaxQueueLength)},
		{name: "trackIds over limit rejected", trackIds: repeatIds(MaxQueueLength + 1), wantErr: true},
		{name: "naturalOrder at limit accepted", trackIds: repeatIds(1), naturalOrder: repeatIds(MaxQueueLength)},
		{name: "naturalOrder over limit rejected", trackIds: repeatIds(1), naturalOrder: repeatIds(MaxQueueLength + 1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQueueState(QueueStateInput{
				UserId:       testUser(),
				TrackIds:     tt.trackIds,
				NaturalOrder: tt.naturalOrder,
			})
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected over-limit queue to be rejected")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want *ValidationError", err)
			}
		})
	}
}

func TestNewQueueState_BoundsStringLength(t *testing.T) {
	huge := strings.Repeat("a", 500*1024) // 500 KiB, far beyond any legit value
	tests := []struct {
		name  string
		input QueueStateInput
	}{
		{
			name:  "oversized trackIds element rejected",
			input: QueueStateInput{UserId: testUser(), TrackIds: []string{huge}},
		},
		{
			name:  "oversized naturalOrder element rejected",
			input: QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, NaturalOrder: []string{huge}},
		},
		{
			name:  "oversized sourceId rejected",
			input: QueueStateInput{UserId: testUser(), SourceId: huge},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQueueState(tt.input)
			if err == nil {
				t.Fatal("expected oversized queue string to be rejected")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want *ValidationError", err)
			}
		})
	}
}

func TestNewQueueState_AcceptsStringAtMaxLength(t *testing.T) {
	atLimit := strings.Repeat("a", MaxQueueStringBytes)
	_, err := NewQueueState(QueueStateInput{
		UserId:   testUser(),
		TrackIds: []string{atLimit},
	})
	if err != nil {
		t.Fatalf("string at max length should be accepted: %v", err)
	}
}

func TestNewQueueState_RejectsNulBytes(t *testing.T) {
	tests := []struct {
		name  string
		input QueueStateInput
	}{
		{
			name:  "nul in trackIds",
			input: QueueStateInput{UserId: testUser(), TrackIds: []string{"a\x00b"}, CurrentIdx: 0},
		},
		{
			name:  "nul in naturalOrder",
			input: QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, NaturalOrder: []string{"x\x00y"}},
		},
		{
			name:  "nul in sourceId",
			input: QueueStateInput{UserId: testUser(), SourceId: "playlist:pid:na\x00me"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQueueState(tt.input)
			if err == nil {
				t.Fatal("expected NUL byte in string element to be rejected")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want *ValidationError", err)
			}
		})
	}
}

func stateFromInput(in QueueStateInput) *QueueState {
	return &QueueState{
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

// Both save paths hand the same field set to one invariant check, and a pair
// mapped to the wrong field there still rejects the same inputs — only the
// field the message names tells them apart.
func TestQueueInvariants_ErrorNamesTheOffendingField(t *testing.T) {
	tests := []struct {
		name    string
		input   QueueStateInput
		wantMsg string
	}{
		{
			name:    "negative positionMs",
			input:   QueueStateInput{TrackIds: []string{"a"}, PositionMs: -1},
			wantMsg: "positionMs must be non-negative",
		},
		{
			name:    "trackIds over limit",
			input:   QueueStateInput{TrackIds: repeatIds(MaxQueueLength + 1)},
			wantMsg: "trackIds length",
		},
		{
			name:    "naturalOrder over limit",
			input:   QueueStateInput{TrackIds: []string{"a"}, NaturalOrder: repeatIds(MaxQueueLength + 1)},
			wantMsg: "naturalOrder length",
		},
		{
			name:    "nul in naturalOrder element",
			input:   QueueStateInput{TrackIds: []string{"a"}, NaturalOrder: []string{"x\x00y"}},
			wantMsg: "naturalOrder contains a NUL byte",
		},
		{
			name:    "nul in sourceId",
			input:   QueueStateInput{TrackIds: []string{"a"}, SourceId: "playlist:pid:na\x00me"},
			wantMsg: "sourceId contains a NUL byte",
		},
		{
			name:    "currentIdx past the end of trackIds",
			input:   QueueStateInput{TrackIds: []string{"a", "b"}, CurrentIdx: 5},
			wantMsg: "currentIdx 5 out of range [0, 2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			in.UserId = testUser()
			_, constructErr := NewQueueState(in)
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
	// #1569: a full save used to store "" at the current slot, after which the
	// client could never use the position-only save for it — NewQueuePosition
	// requires a non-empty currentTrackId — with nothing saying why.
	_, err := NewQueueState(QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", ""},
		CurrentIdx: 1,
	})
	if err == nil {
		t.Fatal("expected an empty current-track id to be rejected")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T, want *ValidationError", err)
	}
}

func TestQueueState_Validate_RejectsEmptyCurrentTrackId(t *testing.T) {
	state := &QueueState{UserId: testUser(), TrackIds: []string{""}, CurrentIdx: 0}

	if err := state.Validate(); err == nil {
		t.Fatal("expected the pre-write check to reject an empty current-track id")
	}
}

func TestNewQueueState_AcceptsRealCurrentTrackId(t *testing.T) {
	state, err := NewQueueState(QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", "b"},
		CurrentIdx: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id, ok := state.CurrentTrackId(); !ok || id != "b" {
		t.Errorf("CurrentTrackId() = (%q, %v), want (%q, true)", id, ok, "b")
	}
}

func TestRehydrateQueueState_KeepsStoredRowWithEmptyCurrentTrackId(t *testing.T) {
	// Rows written before #1569 hold "" at the current slot. Rejecting them on
	// read would classify them corrupt, which resumes the user into an empty
	// queue instead of the real one they saved.
	state, err := RehydrateQueueState(QueueStateInput{
		UserId:     testUser(),
		TrackIds:   []string{"a", ""},
		CurrentIdx: 1,
	}, time.Now())
	if err != nil {
		t.Fatalf("stored row must still load: %v", err)
	}
	if len(state.TrackIds) != 2 {
		t.Errorf("TrackIds = %v, want the stored queue intact", state.TrackIds)
	}
}

// codedValidationError is what a rejected save must present to a client: the
// status to react to and the cause to branch on.
type codedValidationError interface {
	error
	HTTPStatus() int
	ErrorCode() string
}

func validationCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	var coded codedValidationError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T (%v), want a coded validation error", err, err)
	}
	if coded.HTTPStatus() != 400 {
		t.Fatalf("HTTPStatus() = %d, want 400", coded.HTTPStatus())
	}
	return coded.ErrorCode()
}

// Every 400 this module raised used to carry the one code
// "playback.validation_error", so a client could tell an over-long queue from
// an unknown repeat mode only by parsing the detail text (#1596).
func TestQueueStateValidation_EachCauseHasItsOwnCode(t *testing.T) {
	oversized := strings.Repeat("a", MaxQueueStringBytes+1)
	tests := []struct {
		name     string
		reject   func() error
		wantCode string
	}{
		{
			name:     "negative positionMs",
			reject:   rejectedState(QueueStateInput{TrackIds: []string{"a"}, PositionMs: -1}),
			wantCode: "playback.position_ms_negative",
		},
		{
			name:     "trackIds over the length limit",
			reject:   rejectedState(QueueStateInput{TrackIds: repeatIds(MaxQueueLength + 1)}),
			wantCode: "playback.queue_too_long",
		},
		{
			name:     "oversized trackIds element",
			reject:   rejectedState(QueueStateInput{TrackIds: []string{oversized}}),
			wantCode: "playback.string_too_long",
		},
		{
			name:     "NUL byte in sourceId",
			reject:   rejectedState(QueueStateInput{TrackIds: []string{"a"}, SourceId: "playlist:pid:na\x00me"}),
			wantCode: "playback.string_contains_nul",
		},
		{
			name:     "currentIdx past the end of trackIds",
			reject:   rejectedState(QueueStateInput{TrackIds: []string{"a", "b"}, CurrentIdx: 5}),
			wantCode: "playback.current_idx_out_of_range",
		},
		{
			name:     "empty id at currentIdx",
			reject:   rejectedState(QueueStateInput{TrackIds: []string{"a", ""}, CurrentIdx: 1}),
			wantCode: "playback.current_track_id_missing",
		},
		{
			name:     "unknown repeat mode",
			reject:   func() error { _, err := ParseRepeatMode("sometimes"); return err },
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

func rejectedState(in QueueStateInput) func() error {
	return func() error {
		in.UserId = testUser()
		_, err := NewQueueState(in)
		return err
	}
}

// A coded 400 must stay a *ValidationError: that is what the service and
// persistence layers ask errors.As for when classifying a rejected save.
func TestQueueStateValidation_CodedErrorIsStillAValidationError(t *testing.T) {
	_, err := NewQueueState(QueueStateInput{UserId: testUser(), TrackIds: []string{"a"}, PositionMs: -1})

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T (%v), want it to unwrap to *ValidationError", err, err)
	}
}
