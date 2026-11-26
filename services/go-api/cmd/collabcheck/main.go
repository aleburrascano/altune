package main

import (
	"altune/go-api/internal/shared/collabcheck"

	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(collabcheck.Analyzer)
}
