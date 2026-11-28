package contracttest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"context"
	"errors"
	"testing"
)

type LedgerUnderTest interface {
	ports.LibraryTracks
	AddTrack(t *testing.T, track domain.TrackSnapshot)
}

const contractAudioRef = "audio/contract.opus"

func contractTrack(status domain.TrackStatus) domain.TrackSnapshot {
	track := domain.TrackSnapshot{
		ID: contractIDs.NewTrackId(), UserId: contractIDs.NewUserId(),
		Title: "Song", Artist: "Artist", Album: "Album", Status: status,
	}
	if status == domain.TrackReady {
		track.Streamable = true
		return withFileOnRecord(track)
	}
	return withoutFileOnRecord(track)
}

func withFileOnRecord(track domain.TrackSnapshot) domain.TrackSnapshot {
	ref := contractAudioRef
	track.AudioRef = &ref
	return track
}

func withoutFileOnRecord(track domain.TrackSnapshot) domain.TrackSnapshot {
	track.AudioRef = nil
	return track
}

func added(t *testing.T, l LedgerUnderTest, status domain.TrackStatus) domain.TrackSnapshot {
	t.Helper()
	track := contractTrack(status)
	l.AddTrack(t, track)
	return track
}

func ownersView(t *testing.T, l LedgerUnderTest, track domain.TrackSnapshot) domain.TrackSnapshot {
	t.Helper()
	got, err := l.Get(context.Background(), track.ID, track.UserId)
	if err != nil || got == nil {
		t.Fatalf("Get as the owner = %+v, %v; want the track", got, err)
	}
	return *got
}

func someoneElse() ids.UserId { return contractIDs.NewUserId() }

func contractGetFindsATrackOnlyForItsOwner(t *testing.T, l LedgerUnderTest) {
	track := added(t, l, domain.TrackPending)

	if got := ownersView(t, l, track); got.ID != track.ID || got.UserId != track.UserId {
		t.Errorf("Get as the owner = %+v, want track %v of user %v", got, track.ID, track.UserId)
	}
	if got, err := l.Get(context.Background(), track.ID, someoneElse()); got != nil || !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Get as another user = %+v, %v; want ErrTrackNotFound, exactly as for a missing track", got, err)
	}
	if got, err := l.Get(context.Background(), contractIDs.NewTrackId(), track.UserId); got != nil || !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Get of a missing track = %+v, %v; want ErrTrackNotFound", got, err)
	}
}

func wantUntouched(t *testing.T, l LedgerUnderTest, track, before domain.TrackSnapshot) {
	t.Helper()
	after := ownersView(t, l, track)
	if after.Status != before.Status || after.Version != before.Version || after.AudioRef != nil {
		t.Errorf("track after another user's write = %+v, want it untouched: status %v, version %d, no audio", after, before.Status, before.Version)
	}
}

func contractCompleteOnAnotherOwnersTrackIsNotFoundAndWritesNothing(t *testing.T, l LedgerUnderTest) {
	track := added(t, l, domain.TrackPending)
	before := ownersView(t, l, track)

	err := l.Complete(context.Background(), track.ID, someoneElse(), 0, domain.Completion{AudioRef: contractAudioRef})

	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Complete as another user = %v, want ErrTrackNotFound", err)
	}
	wantUntouched(t, l, track, before)
}

func contractFailOnAnotherOwnersTrackIsNotFoundAndWritesNothing(t *testing.T, l LedgerUnderTest) {
	track := added(t, l, domain.TrackPending)
	before := ownersView(t, l, track)

	err := l.Fail(context.Background(), track.ID, someoneElse(), 0, "no_match_found")

	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Fail as another user = %v, want ErrTrackNotFound", err)
	}
	wantUntouched(t, l, track, before)
}

func contractGetReportsEachStatusAndWhetherTheTrackIsStreamable(t *testing.T, l LedgerUnderTest) {
	for _, tt := range []struct {
		status     domain.TrackStatus
		withFile   bool
		streamable bool
	}{
		{domain.TrackPending, false, false},
		{domain.TrackPending, true, false},
		{domain.TrackReady, true, true},
		{domain.TrackReady, false, false},
		{domain.TrackFailed, false, false},
		{domain.TrackFailed, true, false},
	} {
		track := withoutFileOnRecord(contractTrack(tt.status))
		if tt.withFile {
			track = withFileOnRecord(track)
		}
		track.Streamable = tt.streamable
		l.AddTrack(t, track)

		if got := ownersView(t, l, track); got.Status != tt.status || got.Streamable != tt.streamable {
			t.Errorf("a %v track with a file on record %v reads as %v, streamable %v; want %v, streamable %v", tt.status, tt.withFile, got.Status, got.Streamable, tt.status, tt.streamable)
		}
	}
}

func completeWithLength(t *testing.T, l LedgerUnderTest, prior *float64, length music.TrackLength) domain.TrackSnapshot {
	t.Helper()
	track := contractTrack(domain.TrackPending)
	track.StoredDurationSeconds = prior
	l.AddTrack(t, track)
	if err := l.Complete(context.Background(), track.ID, track.UserId, 0, domain.Completion{AudioRef: contractAudioRef, Duration: length}); err != nil {
		t.Fatalf("Complete with length %v = %v, want the track marked ready", length.Seconds(), err)
	}
	got := ownersView(t, l, track)
	if got.Status != domain.TrackReady {
		t.Errorf("after completing with length %v the track is %v, want ready", length.Seconds(), got.Status)
	}
	return got
}

func contractACompletionStoresAKnownLengthAndKeepsTheOldOneOverAnUnknownOne(t *testing.T, l LedgerUnderTest) {
	known, err := music.TrackLengthOf(201.5)
	if err != nil {
		t.Fatalf("TrackLengthOf(201.5) = %v", err)
	}
	if got := completeWithLength(t, l, nil, known); got.StoredDurationSeconds == nil || *got.StoredDurationSeconds != 201.5 {
		t.Errorf("after completing with 201.5 s the stored length is %v, want 201.5", got.StoredDurationSeconds)
	}
	if got := completeWithLength(t, l, nil, music.TrackLength{}); got.StoredDurationSeconds != nil {
		t.Errorf("after completing with an unknown length the stored length is %v, want it left unknown", *got.StoredDurationSeconds)
	}
	prior := 180.0
	if got := completeWithLength(t, l, &prior, music.TrackLength{}); got.StoredDurationSeconds == nil || *got.StoredDurationSeconds != prior {
		t.Errorf("after completing with an unknown length the stored length is %v, want the old %v kept", got.StoredDurationSeconds, prior)
	}
}

var ledgerContractCases = []struct {
	name string
	run  func(t *testing.T, l LedgerUnderTest)
}{
	{"get finds a track only for its owner", contractGetFindsATrackOnlyForItsOwner},
	{"complete on another owner's track is not found and writes nothing", contractCompleteOnAnotherOwnersTrackIsNotFoundAndWritesNothing},
	{"fail on another owner's track is not found and writes nothing", contractFailOnAnotherOwnersTrackIsNotFoundAndWritesNothing},
	{"get reports each status and whether the track is streamable", contractGetReportsEachStatusAndWhetherTheTrackIsStreamable},
	{"the status rules answer the same table for every status and event", contractTheStatusRulesAnswerTheSameTableForEveryStatusAndEvent},
	{"every write is legal exactly when the rules allow the event", contractEveryWriteIsLegalExactlyWhenTheRulesAllowTheEvent},
	{"a completion stores a known length and keeps the old one over an unknown one", contractACompletionStoresAKnownLengthAndKeepsTheOldOneOverAnUnknownOne},
}

func LedgerContract(t *testing.T, newLedger func(t *testing.T) LedgerUnderTest) {
	for _, c := range ledgerContractCases {
		t.Run(c.name, func(t *testing.T) { c.run(t, newLedger(t)) })
	}
}

func contractTheStatusRulesAnswerTheSameTableForEveryStatusAndEvent(t *testing.T, l LedgerUnderTest) {
	p, r, f := domain.TrackPending, domain.TrackReady, domain.TrackFailed
	legal := map[domain.TrackEvent]map[domain.TrackStatus]domain.TrackStatus{
		domain.EventMarkReady:       {p: r, r: r, f: r},
		domain.EventReplaceAudio:    {r: r},
		domain.EventMarkFailed:      {p: f, r: f},
		domain.EventFailPending:     {p: f},
		domain.EventRevertToPending: {r: p, f: p},
	}
	for _, event := range domain.TrackEvents() {
		for _, from := range domain.TrackStatuses() {
			next, err := l.Next(from, event)
			want, ok := legal[event][from]
			switch {
			case !ok && !errors.Is(err, domain.ErrIllegalTransition):
				t.Errorf("event %d from %s: err = %v, want an illegal transition", event, from.Wire(), err)
			case ok && (err != nil || next != want):
				t.Errorf("event %d from %s: next = %s, %v; want %s", event, from.Wire(), next.Wire(), err, want.Wire())
			}
		}
	}
	for name, got := range map[string][]domain.TrackStatus{"claimable": l.Claimable(), "retryable": l.Retryable(), "upgradable": l.Upgradable()} {
		want := map[string]domain.TrackStatus{"claimable": p, "retryable": f, "upgradable": r}[name]
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want [%s]", name, domain.StatusWires(got), want.Wire())
		}
	}
}

func contractEveryWriteIsLegalExactlyWhenTheRulesAllowTheEvent(t *testing.T, l LedgerUnderTest) {
	ctx := context.Background()
	writes := []struct {
		name  string
		event domain.TrackEvent
		do    func(track domain.TrackSnapshot) error
	}{
		{"fail", domain.EventFailPending, func(track domain.TrackSnapshot) error {
			return l.Fail(ctx, track.ID, track.UserId, 0, "no_match_found")
		}},
		{"revert", domain.EventRevertToPending, func(track domain.TrackSnapshot) error { return l.RevertAt(ctx, track) }},
		{"complete", domain.EventMarkReady, func(track domain.TrackSnapshot) error {
			return l.Complete(ctx, track.ID, track.UserId, 0, domain.Completion{AudioRef: contractAudioRef})
		}},
		{"replace", domain.EventReplaceAudio, func(track domain.TrackSnapshot) error {
			return l.Complete(ctx, track.ID, track.UserId, 0, domain.Completion{AudioRef: contractAudioRef + ".new", ReplaceAudio: true})
		}},
	}
	for _, write := range writes {
		for _, from := range domain.TrackStatuses() {
			track := added(t, l, from)
			snapshot := ownersView(t, l, track)

			err := write.do(snapshot)

			next, legal := l.Next(from, write.event)
			if legal != nil {
				if !errors.Is(err, domain.ErrIllegalTransition) {
					t.Errorf("%s from %s: err = %v, want an illegal transition", write.name, from.Wire(), err)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s from %s: err = %v, want it applied", write.name, from.Wire(), err)
				continue
			}
			if got := ownersView(t, l, track); got.Status != next {
				t.Errorf("%s from %s: status = %s, want %s", write.name, from.Wire(), got.Status.Wire(), next.Wire())
			}
		}
	}
}
