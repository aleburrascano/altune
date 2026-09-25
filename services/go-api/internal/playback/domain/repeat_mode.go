package domain

import "fmt"

type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

func (r RepeatMode) String() string {
	switch r {
	case RepeatOff:
		return "off"
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	default:
		return "off"
	}
}

func ParseRepeatMode(s string) (RepeatMode, error) {
	switch s {
	case "off", "":
		return RepeatOff, nil
	case "all":
		return RepeatAll, nil
	case "one":
		return RepeatOne, nil
	default:
		return RepeatOff, newValidationError(codeUnknownRepeatMode, fmt.Sprintf("unknown repeat mode: %q", s))
	}
}
