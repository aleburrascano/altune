package shared

import "github.com/google/uuid"

// EntityId is the shared identity of a catalog entity. It has no text or JSON
// marshaller on purpose: embedding types keep their existing wire encoding.
type EntityId struct {
	value uuid.UUID
}

func NewEntityId() EntityId {
	return EntityId{value: uuid.New()}
}

func ParseEntityId(s string) (EntityId, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return EntityId{}, err
	}
	return EntityId{value: id}, nil
}

func EntityIdFromBytes(b [16]byte) EntityId {
	return EntityId{value: uuid.UUID(b)}
}

func (e EntityId) UUID() uuid.UUID { return e.value }
func (e EntityId) String() string  { return e.value.String() }
func (e EntityId) IsZero() bool    { return e.value == uuid.Nil }
