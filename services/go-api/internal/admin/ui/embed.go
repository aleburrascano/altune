// Package ui holds the embedded Mission Control operator console assets.
package ui

import _ "embed"

// IndexHTML is the operator console page, embedded at compile time so there is
// no runtime file I/O or path dependency.
//
//go:embed index.html
var IndexHTML string
