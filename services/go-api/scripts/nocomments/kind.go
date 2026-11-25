package main

type kind struct {
	name     string
	match    func(path string, head []byte) bool
	comments func(src []byte) ([]span, error)
	same     func(before, after []byte) error

	leftovers func(src []byte) ([]int, error)
}

type span struct{ start, end, line int }

var kinds = []kind{
	caddyfileKind,
	gitPatternKind,
	lineConfigKind,
	npmrcKind,
	cssKind,
	dockerfileKind,
	jsKind,
	markdownKind,
	htmlKind,
	shellKind,
	sqlKind,
	tomlKind,
	workflowKind,
	yamlKind,
}
