package shared

import (
	"errors"

	"github.com/google/uuid"
)

type UserId struct {
	value uuid.UUID
}

var systemUserUUID = uuid.MustParse("00000000-0000-0000-0000-00000000e7a1")

func SystemUserId() UserId {
	return UserId{value: systemUserUUID}
}

func (u UserId) IsSystem() bool {
	return u.value == systemUserUUID
}

var anonymousUserUUID = uuid.MustParse("00000000-0000-0000-0000-0000000a0a0a")

func AnonymousUserId() UserId {
	return UserId{value: anonymousUserUUID}
}

func (u UserId) IsAnonymous() bool {
	return u.value == anonymousUserUUID
}

var ErrSystemUserPersonalization = errors.New("shared: the system user id has no personalization data to read or write")

func GuardNotSystem(userId UserId) error {
	if userId.IsSystem() {
		return ErrSystemUserPersonalization
	}
	return nil
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
