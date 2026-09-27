package main

type kind struct {
	name     string
	match    func(path string, head []byte) bool
	comments func(src []byte) ([]span, error)
	same     func(before, after []byte) error
}

type span struct{ start, end, line int }

var kinds []kind

func register(k kind) { kinds = append(kinds, k) }
