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
			got = withoutG8(got)
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

func TestCollectG4(t *testing.T) {
	handler := "internal/m/adapters/handler/h.go"
	httpError := "package x\n\nfunc serve() { http.Error(w, \"x\", 500) }\n"
	coded := "package x\n\ntype boom struct{}\n\nfunc (*boom) ErrorCode() string { return \"m.boom\" }\n"
	runCases(t, []checkCase{
		{"handler error helper", fullModule("m", map[string]string{handler: "package x\n\nfunc serve() { httputil.HandleServiceError(w, err) }\n"}), nil},
		{"http.Error in handler", fullModule("m", map[string]string{handler: httpError}), []string{"G4 " + handler + ":serve http.Error outside shared/httputil"}},
		{"http.Error in httputil", join(fullModule("m", nil), map[string]string{"internal/shared/httputil/e.go": httpError}), nil},
		{"coded error in domain", fullModule("m", map[string]string{"internal/m/domain/errors.go": coded}), nil},
		{"coded error in service", fullModule("m", map[string]string{"internal/m/service/e.go": coded}), []string{"G4 internal/m/service/e.go boom coded error declared outside domain"}},
	})
}

func TestCollectG5(t *testing.T) {
	handler := "internal/m/adapters/handler/h.go"
	runCases(t, []checkCase{
		{"named constructor", fullModule("m", map[string]string{handler: "package x\n\ntype H struct{}\n\nfunc NewQueueHandler() int { return 1 }\n"}), nil},
		{"bare New", fullModule("m", map[string]string{handler: "package x\n\ntype H struct{}\n\nfunc New() int { return 1 }\n"}), []string{"G5 " + handler + ":New constructor is not New<Resource>Handler"}},
		{"New outside handler", fullModule("m", map[string]string{"internal/m/service/s.go": "package x\n\ntype S struct{}\n\nfunc New() int { return 1 }\n"}), nil},
	})
}

func TestCollectG6(t *testing.T) {
	fake := "package x\n\ntype fakeRepo struct{}\n"
	runCases(t, []checkCase{
		{"fake in one test file", fullModule("m", map[string]string{"internal/m/service/a_test.go": fake}), nil},
		{"fake in two test files", fullModule("m", map[string]string{"internal/m/service/a_test.go": fake, "internal/m/adapters/b_test.go": fake}), []string{"G6 internal/m/service/a_test.go fakeRepo declared in several test files", "G6 internal/m/adapters/b_test.go fakeRepo declared in several test files"}},
		{"stub in two test files", fullModule("m", map[string]string{"internal/m/service/a_test.go": "package x\n\ntype stubRepo struct{}\n", "internal/m/adapters/b_test.go": "package x\n\ntype stubRepo struct{}\n"}), []string{"G6 internal/m/service/a_test.go stubRepo declared in several test files", "G6 internal/m/adapters/b_test.go stubRepo declared in several test files"}},
		{"other types", fullModule("m", map[string]string{"internal/m/service/a_test.go": "package x\n\ntype repo struct{}\n", "internal/m/adapters/b_test.go": "package x\n\ntype repo struct{}\n"}), nil},
	})
}

func TestCollectG7(t *testing.T) {
	logFile := "internal/m/service/log.go"
	logging := func(call string) string { return "package x\n\nfunc run() { " + call + " }\n" }
	runCases(t, []checkCase{
		{"context call", fullModule("m", map[string]string{logFile: logging("slog.InfoContext(ctx, \"x\")")}), nil},
		{"plain call", fullModule("m", map[string]string{logFile: logging("slog.Warn(\"x\")")}), []string{"G7 " + logFile + ":run slog.Warn without context"}},
		{"plain call in test", fullModule("m", map[string]string{"internal/m/service/log_test.go": logging("slog.Warn(\"x\")")}), nil},
	})
}

func conceptTree(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := writeTree(t, files)
	return filepath.Join(repo, "services", "go-api")
}

func TestCollectN2(t *testing.T) {
	doc := "docs/ubiquitous-language.md"
	module := func(name string) map[string]string {
		files := map[string]string{}
		for path, body := range fullModule(name, nil) {
			files["services/go-api/"+path] = body
		}
		return files
	}
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"bold term", join(module("catalog"), map[string]string{doc: "the **catalog** context\n"}), nil},
		{"heading term", join(module("catalog"), map[string]string{doc: "### Catalog\n"}), nil},
		{"unknown term", join(module("gadgets"), map[string]string{doc: "the **catalog** context\n"}), []string{"N2 internal/gadgets is not a term in docs/ubiquitous-language.md"}},
		{"mobile differs", join(module("discovery"), map[string]string{doc: "**Discovery**\n", "client/src/features/discover/index.ts": "x\n"}), []string{"N2 internal/discovery differs from mobile feature discover"}},
		{"mobile same", join(module("playback"), map[string]string{doc: "**Playback**\n", "client/src/features/playback/index.ts": "x\n"}), nil},
		{"no glossary", module("gadgets"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(conceptTree(t, tc.files))
			got = withoutG8(got)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, normalize(tc.want)) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func shapeLine(line string) bool {
	return strings.HasPrefix(line, "G1 ") || strings.HasPrefix(line, "G2 ") || strings.HasPrefix(line, "G3 ")
}

func TestCollectFixtureTree(t *testing.T) {
	got, err := collect("testdata/live")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"G3 internal/acquisition/ports/queue.go:4 imports catalog outside adapters/catalogbridge",
		"G5 internal/observe/handler/handler.go:New constructor is not New<Resource>Handler",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, line := range got {
		if shapeLine(line) && (strings.Contains(line, "internal/playback/adapters/catalogbridge") || strings.Contains(line, "internal/app/")) {
			t.Errorf("unexpected %q", line)
		}
	}
}

func capabilityModule(extra map[string]string) map[string]string {
	files := moduleFiles("m", "domain", "ports")
	files["internal/m/service/run/run.go"] = goSource()
	files["internal/m/adapters/store/store.go"] = goSource()
	return join(files, extra)
}

func capabilityFiles(dir string, count int) map[string]string {
	files := map[string]string{}
	for i := range count {
		files[dir+"/f"+string(rune('a'+i))+".go"] = goSource()
	}
	return files
}

func TestCollectG8(t *testing.T) {
	cases := []checkCase{
		{"observe shaped", capabilityModule(map[string]string{
			"internal/m/service/run/run_test.go":        goSource(),
			"internal/m/adapters/handler/testdata/x.go": goSource(),
			"internal/m/service/_scratch/old.go":        goSource(),
			"internal/m/adapters/.cache/old.go":         goSource(),
		}), nil},
		{"file directly in service", capabilityModule(map[string]string{"internal/m/service/search.go": goSource()}), []string{"G8 internal/m/service/search.go sits directly in service/"}},
		{"file directly in adapters", capabilityModule(map[string]string{"internal/m/adapters/client.go": goSource()}), []string{"G8 internal/m/adapters/client.go sits directly in adapters/"}},
		{"test file directly in service", capabilityModule(map[string]string{"internal/m/service/search_test.go": goSource()}), nil},
		{"files beside packages", capabilityModule(map[string]string{"internal/m/adapters/providers/deezer.go": goSource(), "internal/m/adapters/providers/spotify/spotify.go": goSource()}), []string{"G8 internal/m/adapters/providers/deezer.go sits beside packages"}},
		{"folder of only packages", capabilityModule(map[string]string{"internal/m/adapters/providers/spotify/spotify.go": goSource()}), nil},
		{"too many files", capabilityModule(capabilityFiles("internal/m/service/big", capabilityFileCap+1)), []string{"G8 internal/m/service/big holds more than 16 files"}},
		{"cap is inclusive", capabilityModule(capabilityFiles("internal/m/service/big", capabilityFileCap)), nil},
		{"module name as package", capabilityModule(map[string]string{"internal/m/service/m/m.go": goSource()}), []string{"G8 internal/m/service/m bad package name"}},
		{"layer name as package", capabilityModule(map[string]string{"internal/m/service/ports/p.go": goSource()}), []string{"G8 internal/m/service/ports bad package name"}},
		{"underscore package", capabilityModule(map[string]string{"internal/m/service/foo_bar/f.go": goSource()}), []string{"G8 internal/m/service/foo_bar bad package name"}},
		{"app and shared are not modules", map[string]string{"internal/app/service/a.go": goSource(), "internal/shared/service/a.go": goSource()}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			collected, err := collect(writeTree(t, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			got := slices.DeleteFunc(collected, func(line string) bool { return !strings.HasPrefix(line, "G8 ") })
			if !slices.Equal(got, normalize(tc.want)) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func withoutG8(lines []string) []string {
	return slices.DeleteFunc(lines, func(line string) bool { return strings.HasPrefix(line, "G8 ") })
}
