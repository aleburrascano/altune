package ids_test

import (
	"altune/go-api/internal/shared/ids"
	"testing"

	"github.com/google/uuid"
)

func TestReal_ReturnsADifferentNonZeroUUIDEachCall(t *testing.T) {
	seen := map[uuid.UUID]bool{}

	for range 1000 {
		id := ids.Real()
		if id == uuid.Nil || seen[id] {
			t.Fatalf("Real() = %v, want a new non-zero UUID", id)
		}
		seen[id] = true
	}
}

func TestSource_BuildsEveryIdKindFromTheUUIDItIsGiven(t *testing.T) {
	want := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	src := ids.Source(func() uuid.UUID { return want })

	if got := src.NewUserId().UUID(); got != want {
		t.Errorf("NewUserId = %v, want %v", got, want)
	}
	if got := src.NewTrackId().UUID(); got != want {
		t.Errorf("NewTrackId = %v, want %v", got, want)
	}
	if got := src.NewEntityId().UUID(); got != want {
		t.Errorf("NewEntityId = %v, want %v", got, want)
	}
	if got := src.NewString(); got != want.String() {
		t.Errorf("NewString = %q, want %q", got, want.String())
	}
}

func TestSource_ARealSourceMintsDistinctIds(t *testing.T) {
	src := ids.Source(ids.Real)

	first, second := src.NewTrackId(), src.NewTrackId()

	if first == second {
		t.Fatal("two track ids from a real source are equal")
	}
}
