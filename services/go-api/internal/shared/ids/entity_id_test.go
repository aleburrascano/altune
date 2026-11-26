package ids_test

import (
	"altune/go-api/internal/shared/ids"
	"testing"

	"github.com/google/uuid"
)

func TestEntityIdZero(t *testing.T) {
	if !(ids.EntityId{}).IsZero() {
		t.Error("zero EntityId must report IsZero")
	}
}

func TestParseEntityIdRejectsMalformed(t *testing.T) {
	for _, s := range []string{"not-a-uuid", "00000000-0000-0000-0000-00000000000", ""} {
		if _, err := ids.ParseEntityId(s); err == nil {
			t.Errorf("ParseEntityId(%q) expected error", s)
		}
	}
}

func TestNewEntityIdDiffers(t *testing.T) {
	a, b := ids.NewEntityId(), ids.NewEntityId()
	if a == b || a.IsZero() {
		t.Errorf("expected distinct non-zero ids, got %s and %s", a, b)
	}
}

func TestEntityIdRoundTrip(t *testing.T) {
	const s = "123e4567-e89b-12d3-a456-426614174000"
	id, err := ids.ParseEntityId(s)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != s {
		t.Errorf("String() = %q, want %q", id.String(), s)
	}
	if ids.EntityIdFromBytes(id.UUID()) != id {
		t.Error("EntityIdFromBytes(UUID()) must equal the original")
	}
}

func TestRandomIDIsAUUIDString(t *testing.T) {
	id := ids.RandomID()
	if _, err := uuid.Parse(id); err != nil {
		t.Errorf("RandomID() = %q, not a uuid: %v", id, err)
	}
	if len(id) != 36 {
		t.Errorf("len(RandomID()) = %d, want 36", len(id))
	}
}

func TestRandomIDDiffersBetweenCalls(t *testing.T) {
	if first, second := ids.RandomID(), ids.RandomID(); first == second {
		t.Errorf("two calls returned the same id %q", first)
	}
}
