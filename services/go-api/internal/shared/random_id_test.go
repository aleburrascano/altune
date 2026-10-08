package shared

import (
	"testing"

	"github.com/google/uuid"
)

func TestRandomIDIsAUUIDString(t *testing.T) {
	id := RandomID()
	if _, err := uuid.Parse(id); err != nil {
		t.Errorf("RandomID() = %q, not a uuid: %v", id, err)
	}
	if len(id) != 36 {
		t.Errorf("len(RandomID()) = %d, want 36", len(id))
	}
}

func TestRandomIDDiffersBetweenCalls(t *testing.T) {
	if first, second := RandomID(), RandomID(); first == second {
		t.Errorf("two calls returned the same id %q", first)
	}
}
