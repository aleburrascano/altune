package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func goSource(imports ...string) string {
	body := "package x\n\nimport (\n"
	for _, path := range imports {
		body += "\t\"" + path + "\"\n"
	}
	return body + ")\n"
}

func moduleFiles(module string, present ...string) map[string]string {
	files := map[string]string{
		"internal/" + module + "/ARCHITECTURE.md":                 "x\n",
		"internal/" + module + "/" + module + "test/" + "fake.go": goSource(),
	}
	for _, layer := range present {
		files["internal/"+module+"/"+layer+"/"+layer+".go"] = goSource()
	}
	return files
}

func fullModule(module string, extra map[string]string) map[string]string {
	files := moduleFiles(module, layers...)
	for name, body := range extra {
		files[name] = body
	}
	return files
}

func join(parts ...map[string]string) map[string]string {
	files := map[string]string{}
	for _, part := range parts {
		for name, body := range part {
			files[name] = body
		}
	}
	return files
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type checkCase struct {
	name  string
	files map[string]string
	want  []string
}

func runCases(t *testing.T, cases []checkCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(writeTree(t, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, normalize(tc.want)) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCollectG1(t *testing.T) {
	runCases(t, []checkCase{
		{"full root", fullModule("m", nil), nil},
		{"extra root file", fullModule("m", map[string]string{"internal/m/doc.go": goSource()}), []string{"G1 internal/m/doc.go unexpected root entry"}},
		{
			"look-alike layer",
			join(moduleFiles("m", "ports", "service", "adapters"), map[string]string{"internal/m/domains/d.go": goSource()}),
			[]string{"G1 internal/m missing domain/", "G1 internal/m/domains unexpected root entry"},
		},
		{"app is not a module", map[string]string{"internal/app/doc.go": goSource()}, nil},
	})
}

func TestCollectG1MissingArchitecture(t *testing.T) {
	files := moduleFiles("m", layers...)
	delete(files, "internal/m/ARCHITECTURE.md")
	runCases(t, []checkCase{{"missing architecture", files, []string{"G1 internal/m missing ARCHITECTURE.md"}}})
}

func TestCollectG2(t *testing.T) {
	domainFile := "internal/m/domain/domain.go"
	runCases(t, []checkCase{
		{"stdlib and shared", fullModule("m", map[string]string{domainFile: goSource("fmt", "altune/go-api/internal/shared")}), nil},
		{"own ports", fullModule("m", map[string]string{domainFile: goSource("altune/go-api/internal/m/ports")}), []string{"G2 " + domainFile + ":4 domain imports altune/go-api/internal/m/ports"}},
		{"look-alike shared", fullModule("m", map[string]string{domainFile: goSource("altune/go-api/internal/sharedx")}), []string{"G3 " + domainFile + ":4 imports sharedx outside adapters/sharedxbridge"}},
		{"third party", fullModule("m", map[string]string{domainFile: goSource("github.com/x/y")}), []string{"G2 " + domainFile + ":4 domain imports github.com/x/y"}},
		{"ports may import domain", fullModule("m", map[string]string{"internal/m/ports/ports.go": goSource("altune/go-api/internal/m/domain")}), nil},
		{"service may not import adapters", fullModule("m", map[string]string{"internal/m/service/service.go": goSource("altune/go-api/internal/m/adapters")}), []string{"G2 internal/m/service/service.go:4 service imports altune/go-api/internal/m/adapters"}},
		{"adapters are free", fullModule("m", map[string]string{"internal/m/adapters/adapters.go": goSource("github.com/x/y")}), nil},
		{"unlisted module", fullModule("brandnew", map[string]string{"internal/brandnew/domain/domain.go": goSource("github.com/x/y")}), []string{"G2 internal/brandnew/domain/domain.go:4 domain imports github.com/x/y"}},
		{"test files skipped", fullModule("m", map[string]string{"internal/m/domain/domain_test.go": goSource("github.com/x/y")}), nil},
	})
}

func TestCollectG3(t *testing.T) {
	bridgeFile := "internal/a/adapters/bbridge/bridge.go"
	other := fullModule("b", nil)
	runCases(t, []checkCase{
		{"bridge reaches ports", join(fullModule("a", map[string]string{bridgeFile: goSource("altune/go-api/internal/b/ports")}), other), nil},
		{"service reaches domain", join(fullModule("a", map[string]string{"internal/a/service/service.go": goSource("altune/go-api/internal/b/domain")}), other), []string{"G3 internal/a/service/service.go:4 imports b outside adapters/bbridge"}},
		{"bridge reaches own ports", join(fullModule("a", map[string]string{bridgeFile: goSource("altune/go-api/internal/a/ports")}), other), []string{"G3 internal/a/adapters/bbridge imports nothing from b ports or service"}},
		{"look-alike bridge", join(fullModule("a", map[string]string{"internal/a/adapters/bbridgex/bridge.go": goSource("altune/go-api/internal/b/ports")}), other), []string{"G3 internal/a/adapters/bbridgex/bridge.go:4 imports b outside adapters/bbridge"}},
		{"app and shared are not cross", fullModule("a", map[string]string{"internal/a/adapters/adapters.go": goSource("altune/go-api/internal/app", "altune/go-api/internal/shared")}), nil},
	})
}

func TestCollectN1(t *testing.T) {
	runCases(t, []checkCase{
		{"plain name", map[string]string{"thing.go": goSource()}, nil},
		{"banned name", map[string]string{"common.go": goSource()}, []string{"N1 common.go banned name common"}},
		{"banned name with test", map[string]string{"common.go": goSource(), "common_test.go": goSource()}, []string{"N1 common.go banned name common"}},
		{"banned test only", map[string]string{"common_test.go": goSource()}, []string{"N1 common_test.go banned name common"}},
		{"look-alikes", map[string]string{"commonality.go": goSource(), "libraries/x.go": goSource()}, nil},
		{"shell script", map[string]string{"deploy/lib.sh": "x\n"}, []string{"N1 deploy/lib.sh banned name lib"}},
		{"banned folder", map[string]string{"utils/x.go": goSource()}, []string{"N1 utils banned name utils"}},
		{"skipped folders", map[string]string{"vendor/lib.go": goSource(), "testdata/lib.go": goSource(), ".git/lib": "x", "node_modules/lib.js": "x"}, nil},
	})
}

func TestCollectLive(t *testing.T) {
	got, err := collect("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"G1 internal/auth missing domain/",
		"G1 internal/catalog/doc.go unexpected root entry",
		"G3 internal/acquisition/ports/queue.go:4 imports catalog outside adapters/catalogbridge",
		"G3 internal/discovery/adapters/catalogbridge imports nothing from catalog ports or service",
		"N1 cmd/api/commands/common.go banned name common",
		"N1 deploy/lib.sh banned name lib",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, line := range got {
		if strings.Contains(line, "internal/playback/adapters/catalogbridge") || strings.Contains(line, "internal/app/") {
			t.Errorf("unexpected %q", line)
		}
	}
}
