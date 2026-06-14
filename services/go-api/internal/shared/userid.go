package shared

import "github.com/google/uuid"

type UserId struct {
	value uuid.UUID
}

func NewUserId(id uuid.UUID) UserId {
	return UserId{value: id}
}

func ParseUserId(s string) (UserId, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return UserId{}, err
	}
	return UserId{value: id}, nil
}

func (u UserId) UUID() uuid.UUID {
	return u.value
}

func (u UserId) String() string {
	return u.value.String()
}

func (u UserId) IsZero() bool {
	return u.value == uuid.Nil
}
