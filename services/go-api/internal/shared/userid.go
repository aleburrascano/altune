package shared

import "github.com/google/uuid"

type UserId struct {
	value uuid.UUID
}

// systemUserUUID is the synthetic identity used by internal background jobs
// (e.g. the smoke-eval runner). It is not a real account: work performed under
// it must never read or write any real user's personalization data.
var systemUserUUID = uuid.MustParse("00000000-0000-0000-0000-00000000e7a1")

// SystemUserId returns the synthetic system identity. Use it for internal jobs
// that run the real per-user code paths but must not touch a real user's signal.
func SystemUserId() UserId {
	return UserId{value: systemUserUUID}
}

// IsSystem reports whether this is the synthetic system identity.
func (u UserId) IsSystem() bool {
	return u.value == systemUserUUID
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
