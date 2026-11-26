package ids

import "github.com/google/uuid"

type Source func() uuid.UUID

func Real() uuid.UUID {
	return uuid.New()
}

func (s Source) NewUserId() UserId {
	return NewUserId(s())
}

func (s Source) NewTrackId() TrackId {
	return TrackId{EntityId{value: s()}}
}

func (s Source) NewEntityId() EntityId {
	return EntityId{value: s()}
}

func (s Source) NewString() string {
	return s().String()
}
