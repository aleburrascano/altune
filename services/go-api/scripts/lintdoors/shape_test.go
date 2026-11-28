package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shapeTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example/api\n\ngo 1.26\n")
	writeFile(t, dir, "internal/app/app.go", "package app\n")
	writeFile(t, dir, "internal/shared/clock/clock.go", "package clock\n")
	writeFile(t, dir, "internal/lib/lib.go", "package lib\n\nfunc New() {}\n")
	writeFile(t, dir, "internal/lib/README.md", "`lib` owns the one thing it does for its callers.\n\n## Must not\n")
	writeFile(t, dir, "internal/lib/CLAUDE.md", "@README.md\n")
	writeFile(t, dir, "internal/lib/libtest/libtest.go", "package libtest\n")
	writeFile(t, dir, "internal/lib/internal/domain/errors.go", "package domain\n")
	writeFile(t, dir, "internal/lib/internal/service/use.go", "package service\n")
	writeFile(t, dir, "internal/lib/internal/ports/ports.go", "package ports\n")
	writeFile(t, dir, "internal/lib/internal/contracttest/c.go", "package contracttest\n")
	writeFile(t, dir, "internal/lib/internal/adapters/handler/routes.go", "package handler\n\nimport _ \"github.com/go-chi/chi/v5\"\n")
	writeFile(t, dir, "internal/lib/internal/adapters/handler/dto.go", "package handler\n\ntype trackDTO struct {\n\tID string `json:\"id\"`\n}\n")
	writeFile(t, dir, "internal/lib/internal/adapters/handler/errors.go", "package handler\n\nimport _ \"example/api/internal/shared/apierror\"\n")
	writeFile(t, dir, "internal/lib/internal/adapters/persistence/track_repo.go", "package persistence\n")
	writeFile(t, dir, "internal/lib/internal/adapters/memory/track_repo.go", "package memory\n")
	writeFile(t, dir, "internal/lib/internal/adapters/cache/entry.go", "package cache\n\ntype entry struct {\n\tKey string `json:\"key\"`\n}\n")
	writeFile(t, dir, "internal/platform/small/small.go", "package small\n\nfunc New() {}\n")
	writeFile(t, dir, "internal/platform/small/README.md", "`small` owns one narrow job for the whole server.\n")
	writeFile(t, dir, "internal/platform/small/internal/service/run.go", "package service\n")
	return dir
}

func shape(t *testing.T, dir string) (int, string) {
	t.Helper()
	code, out, stderr := lint(t, "--shape", dir)
	if code == 2 {
		t.Fatalf("shape scan failed: %s", stderr)
	}
	return code, out
}

func TestShapeAcceptsACleanTree(t *testing.T) {
	code, out := shape(t, shapeTree(t))

	if code != 0 || !strings.Contains(out, "module shape violations: 0") {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
}

func TestShapeRefusesWhatTheTemplateForbids(t *testing.T) {
	cases := map[string]struct {
		section string
		change  func(t *testing.T, dir string)
		want    string
	}{
		"rule 1: a folder at the module root": {
			sectionRoot, func(t *testing.T, dir string) { writeFile(t, dir, "internal/lib/service/s.go", "package service\n") },
			"folder service at the module root",
		},
		"rule 1: a stray file at the module root": {
			sectionRoot, func(t *testing.T, dir string) { writeFile(t, dir, "internal/lib/notes.txt", "x") },
			"file notes.txt at the module root",
		},
		"rule 1: no internal folder": {
			sectionRoot, func(t *testing.T, dir string) { removeAll(t, dir, "internal/platform/small/internal") },
			"has no internal/ folder",
		},
		"rule 2: a second door file": {
			sectionDoor, func(t *testing.T, dir string) { writeFile(t, dir, "internal/lib/lib_door.go", "package lib\n") },
			"second door file lib_door.go",
		},
		"rule 2: no door file": {
			sectionDoor, func(t *testing.T, dir string) { removeAll(t, dir, "internal/lib/lib.go") },
			"has no door file lib.go",
		},
		"rule 2: the door has no New": {
			sectionDoor, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/lib.go", "package lib\n\nfunc Build() {}\n")
			},
			"declares no func New",
		},
		"rule 2: a method named New is not the constructor": {
			sectionDoor, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/lib.go", "package lib\n\ntype T struct{}\n\nfunc (T) New() {}\n")
			},
			"declares no func New",
		},
		"rule 5: the service imports an adapter": {
			sectionCore, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/wired.go", "package service\n\nimport _ \"example/api/internal/lib/internal/adapters/memory\"\n")
			},
			"service imports an adapter",
		},
		"rule 5: the domain imports an adapter": {
			sectionCore, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/domain/wired.go", "package domain\n\nimport _ \"example/api/internal/lib/internal/adapters/cache\"\n")
			},
			"domain imports an adapter",
		},
		"rule 7: a package folder beside the layers": {
			sectionLayers, func(t *testing.T, dir string) { writeFile(t, dir, "internal/lib/internal/core/c.go", "package core\n") },
			"package folder core under internal/",
		},
		"rule 7: a package nested below domain": {
			sectionLayers, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/domain/track/track.go", "package track\n")
			},
			"nested below domain",
		},
		"rule 7: a package nested below service": {
			sectionLayers, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/sub/s.go", "package sub\n")
			},
			"nested below service",
		},
		"rule 7: a package nested below an adapter": {
			sectionLayers, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/adapters/cache/inner/i.go", "package inner\n")
			},
			"nested below cache",
		},
		"rule 7: a file directly in internal": {
			sectionLayers, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/ports.go", "package internal\n")
			},
			"file ports.go directly in internal/",
		},
		"rule 10: no domain errors.go": {
			sectionFileMap, func(t *testing.T, dir string) { removeAll(t, dir, "internal/lib/internal/domain/errors.go") },
			"domain/ has no errors.go",
		},
		"rule 10: no ports.go": {
			sectionFileMap, func(t *testing.T, dir string) { removeAll(t, dir, "internal/lib/internal/ports/ports.go") },
			"ports/ has no ports.go",
		},
		"rule 10: the handler has no routes.go": {
			sectionFileMap, func(t *testing.T, dir string) {
				removeAll(t, dir, "internal/lib/internal/adapters/handler/routes.go")
				writeFile(t, dir, "internal/lib/internal/adapters/handler/mount.go", "package handler\n\nimport _ \"github.com/go-chi/chi/v5\"\n")
			},
			"has no routes.go",
		},
		"rule 10: the handler has no dto.go": {
			sectionFileMap, func(t *testing.T, dir string) {
				removeAll(t, dir, "internal/lib/internal/adapters/handler/dto.go")
				writeFile(t, dir, "internal/lib/internal/adapters/handler/shapes.go", "package handler\n\ntype x struct {\n\tID string `json:\"id\"`\n}\n")
			},
			"has no dto.go",
		},
		"rule 10: the handler has no errors.go": {
			sectionFileMap, func(t *testing.T, dir string) { removeAll(t, dir, "internal/lib/internal/adapters/handler/errors.go") },
			"has no errors.go",
		},
		"rule 10: a repository without its memory fake": {
			sectionFileMap, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/adapters/persistence/album_repo.go", "package persistence\n")
			},
			"has no adapters/memory/album_repo.go",
		},
		"rule 10: a json tag in the domain": {
			sectionFileMap, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/domain/track.go", "package domain\n\ntype Track struct {\n\tID string `json:\"id\"`\n}\n")
			},
			"json tag outside a wire or stored shape",
		},
		"rule 10: a json tag in the door": {
			sectionFileMap, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/lib.go", "package lib\n\nfunc New() {}\n\ntype Out struct {\n\tID string `json:\"id\"`\n}\n")
			},
			"json tag outside a wire or stored shape",
		},
		"rule 10: a json tag in a handler file that is not dto.go": {
			sectionFileMap, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/adapters/handler/other.go", "package handler\n\ntype x struct {\n\tID string `json:\"id\"`\n}\n")
			},
			"json tag outside a wire or stored shape",
		},
		"rule 10: apierror imported by the service": {
			sectionFileMap, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/err.go", "package service\n\nimport _ \"example/api/internal/shared/apierror\"\n")
			},
			"imports apierror outside adapters/handler",
		},
		"rule 13: a setter on a pointer receiver": {
			sectionSetters, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/limits.go", "package service\n\ntype S struct{ n int }\n\nfunc (s *S) WithRateLimits(n int) *S { s.n = n; return s }\n")
			},
			"exported method WithRateLimits",
		},
		"rule 18: a package-level var that is reassigned": {
			sectionGlobals, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/state.go", "package service\n\nvar current = 1\n\nfunc Bump() { current++ }\n")
			},
			"package-level var current is changed at runtime",
		},
		"rule 18: a package-level map written to": {
			sectionGlobals, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/cache.go", "package service\n\nvar seen = map[string]bool{}\n\nfunc Mark(k string) { seen[k] = true }\n")
			},
			"package-level var seen is changed at runtime",
		},
		"rule 18: a package-level sync.Once": {
			sectionGlobals, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/once.go", "package service\n\nimport \"sync\"\n\nvar once sync.Once\n")
			},
			"package-level once holds runtime state",
		},
		"rule 18: a package-level var changed from another file": {
			sectionGlobals, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/service/a.go", "package service\n\nvar level = 1\n")
				writeFile(t, dir, "internal/lib/internal/service/b.go", "package service\n\nfunc Raise() { level = 2 }\n")
			},
			"package-level var level is changed at runtime",
		},
		"rule 20: a util package": {
			sectionNames, func(t *testing.T, dir string) { writeFile(t, dir, "internal/shared/util/u.go", "package util\n") },
			`"util" says nothing`,
		},
		"rule 20: a helpers package in a module": {
			sectionNames, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/internal/adapters/helpers/h.go", "package helpers\n")
			},
			`"helpers" says nothing`,
		},
		"rule 20: a common package": {
			sectionNames, func(t *testing.T, dir string) { writeFile(t, dir, "internal/shared/common/c.go", "package common\n") },
			`"common" says nothing`,
		},
		"rule 20: a misc package": {
			sectionNames, func(t *testing.T, dir string) { writeFile(t, dir, "internal/shared/misc/m.go", "package misc\n") },
			`"misc" says nothing`,
		},
		"rule 20: a package name with an underscore": {
			sectionNames, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/shared/track_ids/t.go", "package track_ids\n")
			},
			"is not one plain lowercase word",
		},
		"rule 20: a package name in camel case": {
			sectionNames, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/shared/trackIDs/t.go", "package trackIDs\n")
			},
			"is not one plain lowercase word",
		},
		"rule 21: no README": {
			sectionReadme, func(t *testing.T, dir string) { removeAll(t, dir, "internal/lib/README.md") },
			"has no README.md",
		},
		"rule 21: line 1 is a heading": {
			sectionReadme, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/README.md", "# lib\n\nIt owns the one thing it does.\n")
			},
			"line 1 is not a sentence",
		},
		"rule 21: line 1 has no full stop": {
			sectionReadme, func(t *testing.T, dir string) {
				writeFile(t, dir, "internal/lib/README.md", "`lib` owns the one thing it does for its callers\n")
			},
			"line 1 is not a sentence",
		},
		"rule 21: line 1 is too short to say a job": {
			sectionReadme, func(t *testing.T, dir string) { writeFile(t, dir, "internal/lib/README.md", "Library.\n") },
			"line 1 is not a sentence",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := shapeTree(t)
			tc.change(t, dir)

			code, out := shape(t, dir)

			if code != 1 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit = %d, want 1 with %q; output:\n%s", code, tc.want, out)
			}
			if !strings.Contains(out, `read services/go-api/README.md, section "`+tc.section+`"`) {
				t.Fatalf("the message does not name the README section %q:\n%s", tc.section, out)
			}
		})
	}
}

func TestShapeAcceptsWhatTheTemplateAllows(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"a door test at the root": func(t *testing.T, dir string) { writeFile(t, dir, "internal/lib/lib_test.go", "package lib_test\n") },
		"a testdata folder inside an adapter": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/adapters/cache/testdata/a/b.json", "{}")
		},
		"a stored shape with tags in a cache adapter": func(t *testing.T, dir string) {},
		"a repository pair": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/adapters/persistence/album_repo.go", "package persistence\n")
			writeFile(t, dir, "internal/lib/internal/adapters/memory/album_repo.go", "package memory\n")
		},
		"a handler without routes or wire shapes": func(t *testing.T, dir string) {
			removeAll(t, dir, "internal/lib/internal/adapters/handler/routes.go")
			removeAll(t, dir, "internal/lib/internal/adapters/handler/dto.go")
			writeFile(t, dir, "internal/lib/internal/adapters/handler/mw.go", "package handler\n")
		},
		"a stdlib With method on a pointer receiver": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/service/h.go", "package service\n\ntype H struct{}\n\nfunc (h *H) WithAttrs(a []int) *H { return h }\n\nfunc (h *H) WithGroup(n string) *H { return h }\n")
		},
		"a With method that returns a changed copy": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/domain/r.go", "package domain\n\ntype R struct{ n int }\n\nfunc (r R) WithExtra(n int) R { r.n = n; return r }\n")
		},
		"a package-level table that is only read": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/domain/table.go", "package domain\n\nvar kinds = map[string]int{\"a\": 1}\n\nfunc Kind(k string) int { return kinds[k] }\n")
		},
		"a local that shadows a package-level name": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/domain/shadow.go", "package domain\n\nvar level = 1\n\nfunc Count() int { level := 0; level++; return level }\n")
		},
		"a package-level var changed only in a test": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/domain/seam.go", "package domain\n\nvar hook = func() {}\n")
			writeFile(t, dir, "internal/lib/internal/domain/seam_test.go", "package domain\n\nfunc init() { hook = nil }\n")
		},
		"a sync.Once held by a struct that New builds": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/service/closer.go", "package service\n\nimport \"sync\"\n\ntype closer struct{ once sync.Once }\n")
		},
		"a folder with a banned name that holds no code": func(t *testing.T, dir string) {
			writeFile(t, dir, "internal/lib/internal/adapters/cache/testdata/util/data.json", "{}")
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			dir := shapeTree(t)
			change(t, dir)

			code, out := shape(t, dir)

			if code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestShapeLeavesSharedAndAppOutOfTheModuleRules(t *testing.T) {
	dir := shapeTree(t)
	writeFile(t, dir, "internal/shared/clock/extra.go", "package clock\n")
	writeFile(t, dir, "internal/app/jobs/jobs.go", "package jobs\n")

	code, out := shape(t, dir)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
	}
}

func TestEveryCitedSectionExistsInTheREADME(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read the go-api README: %v", err)
	}
	for _, section := range allSections {
		if !strings.Contains(string(body), "\n### "+section+"\n") {
			t.Errorf("the go-api README has no heading %q", section)
		}
	}
}

func TestShapeReportsBadInput(t *testing.T) {
	if code, _, stderr := lint(t, "--shape", t.TempDir()); code != 2 || !strings.Contains(stderr, "lintdoors:") {
		t.Fatalf("no go.mod: exit = %d, stderr %q", code, stderr)
	}
}

func removeAll(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}
