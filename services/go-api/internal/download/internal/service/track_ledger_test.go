package service_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

type libraryHalf struct {
	*memory.Ledger
	accepts bool
}

func (l libraryHalf) EvidenceAccepted(domain.Completion) bool { return l.accepts }

type ledgerFixture struct {
	library *memory.Ledger
	state   *memory.UpgradeState
	ledger  *service.TrackLedger
	id      ids.TrackId
	user    ids.UserId
}

func newLedgerFixture(t *testing.T, accepts bool, seed func(*domain.TrackSnapshot)) ledgerFixture {
	t.Helper()
	user := ids.NewUserId(uuid.New())
	snapshot := domain.TrackSnapshot{ID: ids.NewTrackId(), UserId: user, Title: "Title", Artist: "Artist", Status: domain.TrackPending}
	if seed != nil {
		seed(&snapshot)
	}
	library := memory.NewLedger(memory.LedgerTrack{TrackSnapshot: snapshot})
	state := memory.NewUpgradeState(time.Now)
	ledger, err := service.NewTrackLedger(libraryHalf{Ledger: library, accepts: accepts}, state)
	if err != nil {
		t.Fatal(err)
	}
	return ledgerFixture{library: library, state: state, ledger: ledger, id: snapshot.ID, user: user}
}

func TestGetOverlaysTheUpgradeStateOnTheTrackEvidence(t *testing.T) {
	f := newLedgerFixture(t, true, func(s *domain.TrackSnapshot) {
		s.Evidence = json.RawMessage(`{"grade":"B","upgrade_tries":9}`)
	})
	f.state.Seed(f.id, domain.UpgradeStateSnapshot{Evidence: json.RawMessage(`{"upgrade_tries":2}`), RejectedSourceKeys: []string{"bad"}})

	snap, err := f.ledger.Get(context.Background(), f.id, f.user)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(snap.Evidence, &got); err != nil {
		t.Fatalf("evidence = %s: %v", snap.Evidence, err)
	}
	if got["grade"] != "B" || got["upgrade_tries"] != float64(2) {
		t.Errorf("evidence = %v, want the track's grade and the state's upgrade_tries", got)
	}
	if len(snap.RejectedSourceKeys) != 1 || snap.RejectedSourceKeys[0] != "bad" {
		t.Errorf("rejected keys = %v, want the state's", snap.RejectedSourceKeys)
	}
}

func TestGetReturnsTheTrackNotFoundOfTheLibraryHalfWithoutReadingTheState(t *testing.T) {
	f := newLedgerFixture(t, true, nil)
	f.state.FailWith(errors.New("state must not be read"))

	_, err := f.ledger.Get(context.Background(), f.id, ids.NewUserId(uuid.New()))

	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Fatalf("Get = %v, want ErrTrackNotFound", err)
	}
}

func TestCompleteMarksPendingTrackReadyAndSettlesTheRejectedKeys(t *testing.T) {
	f := newLedgerFixture(t, true, nil)

	err := f.ledger.Complete(context.Background(), f.id, f.user, 0, domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad-key"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(f.state.Settled()) != 1 || len(f.state.Settled()[0].Keys) != 1 || f.state.Settled()[0].Keys[0] != "bad-key" || !f.state.Settled()[0].Reset {
		t.Fatalf("upgrade state settled = %+v, want the rejected key and a reset for the replaced evidence", f.state.Settled())
	}
	if got, _ := f.library.Track(f.id); got.Status != domain.TrackReady {
		t.Errorf("status = %v, want ready", got.Status)
	}
}

func TestCompleteSettlesNothingWhenThereIsNoRejectionAndNoNewEvidence(t *testing.T) {
	f := newLedgerFixture(t, false, nil)

	err := f.ledger.Complete(context.Background(), f.id, f.user, 0, domain.Completion{AudioRef: "ref-1"})

	if err != nil || len(f.state.Settled()) != 0 {
		t.Errorf("Complete = %v, settled = %+v, want success and no state write", err, f.state.Settled())
	}
}

func TestCompleteReturnsTheStateErrorAndLeavesTheTrackUntouchedSoTheSchedulerRetries(t *testing.T) {
	f := newLedgerFixture(t, true, nil)
	stateErr := errors.New("state unavailable")
	f.state.FailWith(stateErr)

	err := f.ledger.Complete(context.Background(), f.id, f.user, 0, domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad"}})

	if !errors.Is(err, stateErr) {
		t.Fatalf("Complete = %v, want the state error", err)
	}
	if got, _ := f.library.Track(f.id); got.Status != domain.TrackPending {
		t.Errorf("status = %v, want the track untouched because the library write never ran", got.Status)
	}
}

func TestCompleteThatFailsAfterTheStateWriteIsRetriedWithTheSameStateWrite(t *testing.T) {
	f := newLedgerFixture(t, true, nil)
	completion := domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad"}, Confidence: 0.5, Evidence: json.RawMessage(`{"k":1}`)}
	f.library.FailWrites(errors.New("library down"))

	if err := f.ledger.Complete(context.Background(), f.id, f.user, 0, completion); err == nil {
		t.Fatal("Complete = nil, want the library write error")
	}
	f.library.FailWrites(nil)
	if err := f.ledger.Complete(context.Background(), f.id, f.user, 0, completion); err != nil {
		t.Fatalf("retried Complete = %v", err)
	}

	if len(f.state.Settled()) != 2 || !reflect.DeepEqual(f.state.Settled()[0], f.state.Settled()[1]) {
		t.Errorf("settled = %+v, want the identical idempotent write twice", f.state.Settled())
	}
	if got, _ := f.library.Track(f.id); got.Status != domain.TrackReady {
		t.Errorf("status = %v, want ready after the retry", got.Status)
	}
}

func TestCompleteHandsTheClaimFenceToTheStateWriteAndReturnsALostLease(t *testing.T) {
	f := newLedgerFixture(t, true, nil)
	f.state.FailWith(domain.ErrLeaseLost)

	err := f.ledger.Complete(context.Background(), f.id, f.user, 4, domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad"}})

	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("Complete = %v, want ErrLeaseLost from the fenced state write", err)
	}
	if len(f.state.Settled()) != 1 || f.state.Settled()[0].Fence != 4 {
		t.Errorf("settled = %+v, want one write carrying fence 4", f.state.Settled())
	}
	if got, _ := f.library.Track(f.id); got.Status != domain.TrackPending {
		t.Errorf("status = %v, want pending because the library write never ran", got.Status)
	}
}

func TestCompleteResetsTheUpgradeEvidenceOnlyWhenTheLibraryAcceptsTheNewEvidence(t *testing.T) {
	cases := []struct {
		name      string
		accepts   bool
		wantReset bool
	}{
		{"evidence the library accepts", true, true},
		{"evidence the library refuses", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newLedgerFixture(t, tc.accepts, nil)

			err := f.ledger.Complete(context.Background(), f.id, f.user, 0, domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(f.state.Settled()) != 1 || f.state.Settled()[0].Reset != tc.wantReset {
				t.Errorf("settled = %+v, want one write with reset = %v", f.state.Settled(), tc.wantReset)
			}
		})
	}
}

func TestCompleteByAnotherOwnerWritesNoUpgradeState(t *testing.T) {
	f := newLedgerFixture(t, true, nil)

	err := f.ledger.Complete(context.Background(), f.id, ids.NewUserId(uuid.New()), 1, domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad"}})

	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Fatalf("Complete = %v, want ErrTrackNotFound", err)
	}
	if len(f.state.Settled()) != 0 {
		t.Errorf("settled = %+v, want no state write for another owner's track", f.state.Settled())
	}
}

func TestNewTrackLedgerRefusesAMissingLibraryHalfOrUpgradeState(t *testing.T) {
	library := libraryHalf{Ledger: memory.NewLedger()}
	cases := map[string]func() (*service.TrackLedger, error){
		"no library half": func() (*service.TrackLedger, error) {
			return service.NewTrackLedger(nil, memory.NewUpgradeState(time.Now))
		},
		"no upgrade state": func() (*service.TrackLedger, error) { return service.NewTrackLedger(library, nil) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if ledger, err := build(); err == nil || ledger != nil {
				t.Errorf("NewTrackLedger = %v, %v, want a refusal", ledger, err)
			}
		})
	}
}

func TestTrackLedgerKeepsTheUpgradeStateOfAMemoryStateAcrossGetAndComplete(t *testing.T) {
	user := ids.NewUserId(uuid.New())
	id := ids.NewTrackId()
	library := libraryHalf{Ledger: memory.NewLedger(memory.LedgerTrack{TrackSnapshot: domain.TrackSnapshot{ID: id, UserId: user, Status: domain.TrackPending}}), accepts: true}
	ledger, err := service.NewTrackLedger(library, memory.NewUpgradeState(time.Now))
	if err != nil {
		t.Fatal(err)
	}

	if err := ledger.Complete(context.Background(), id, user, 0, domain.Completion{AudioRef: "ref-1", RejectedSourceKeys: []string{"bad"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := ledger.Get(context.Background(), id, user)
	if err != nil {
		t.Fatal(err)
	}

	if len(snap.RejectedSourceKeys) != 1 || snap.RejectedSourceKeys[0] != "bad" {
		t.Errorf("rejected keys = %v, want the settled key read back through the memory state", snap.RejectedSourceKeys)
	}
}
