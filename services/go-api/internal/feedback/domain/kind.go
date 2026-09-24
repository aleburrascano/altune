package domain

import (
	"fmt"
)

type Kind int

const (
	KindBug Kind = iota
	KindIdea
	KindConfusing
)

var kinds = map[Kind]string{
	KindBug:       "bug",
	KindIdea:      "idea",
	KindConfusing: "confusing",
}

func (k Kind) String() string {
	if name, ok := kinds[k]; ok {
		return name
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

func (k Kind) Valid() bool {
	_, ok := kinds[k]
	return ok
}

func ParseKind(s string) (Kind, error) {
	for kind, name := range kinds {
		if name == s {
			return kind, nil
		}
	}
	return KindBug, NewValidationError(fmt.Sprintf("unknown kind: %q", truncate(s, maxKindEchoRunes)))
}
