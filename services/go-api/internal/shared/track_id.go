package shared

type TrackId struct{ EntityId }

func NewTrackId() TrackId {
	return TrackId{NewEntityId()}
}

func ParseTrackId(s string) (TrackId, error) {
	id, err := ParseEntityId(s)
	if err != nil {
		return TrackId{}, err
	}
	return TrackId{id}, nil
}

func TrackIdFromUUID(id [16]byte) TrackId {
	return TrackId{EntityIdFromBytes(id)}
}
