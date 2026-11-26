package idstest

import (
	"altune/go-api/internal/shared/ids"
	"encoding/binary"
	"sync/atomic"

	"github.com/google/uuid"
)

func Sequence() ids.Source {
	var next atomic.Uint64
	return func() uuid.UUID {
		var id uuid.UUID
		binary.BigEndian.PutUint64(id[8:], next.Add(1))
		return id
	}
}
