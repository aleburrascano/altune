package streamrip

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func identityWith(provider, externalID, url string) ports.RecordingIdentity {
	return ports.RecordingIdentity{
		Duration: 200,
		Sources:  []ports.RecordingSource{{Provider: provider, ExternalID: externalID, URL: url}},
	}
}

func TestFind_BuildsTrackURLPerService(t *testing.T) {
	tests := []struct {
		service string
		id      string
		want    string
	}{
		{"tidal", "103805726", "https://tidal.com/browse/track/103805726"},
		{"deezer", "3135556", "https://www.deezer.com/track/3135556"},
		{"qobuz", "12345", "https://open.qobuz.com/track/12345"},
	}

	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			src := NewSource(tt.service)
			got, err := src.Find(context.Background(), ports.FindRequest{
				Title:    "Song",
				Identity: identityWith(tt.service, tt.id, ""),
			})
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("candidates = %d, want 1", len(got))
			}
			if got[0].URL != tt.want {
				t.Errorf("URL = %q, want %q", got[0].URL, tt.want)
			}
			if !got[0].Resolved {
				t.Error("a catalog-resolved candidate must be marked Resolved")
			}
		})
	}
}

func TestFind_SoundCloudUsesThePermalinkNotAnID(t *testing.T) {
	src := NewSource("soundcloud")

	got, err := src.Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("soundcloud", "999", "https://soundcloud.com/artist/song"),
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://soundcloud.com/artist/song" {
		t.Fatalf("candidates = %+v, want the permalink", got)
	}
}

func TestFind_SoundCloudAcceptsOnlyItsOwnHostsOverHTTPS(t *testing.T) {
	permalinks := []string{
		"https://soundcloud.com/a/b",
		"https://www.soundcloud.com/a/b",
		"https://m.soundcloud.com/a/b",
		"https://SoundCloud.com/a/b",
	}

	for _, permalink := range permalinks {
		t.Run(permalink, func(t *testing.T) {
			src := NewSource("soundcloud")
			got, err := src.Find(context.Background(), ports.FindRequest{
				Title:    "Song",
				Identity: identityWith("soundcloud", "999", permalink),
			})
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(got) != 1 || got[0].URL != permalink {
				t.Fatalf("candidates = %+v, want the permalink %q", got, permalink)
			}
		})
	}
}

// TestFind_SoundCloudRejectsAForeignFetchTarget covers the request-forgery class:
// the permalink is third-party provider data handed straight to rip, so any host
// or scheme but SoundCloud over https must produce no candidate at all.
func TestFind_SoundCloudRejectsAForeignFetchTarget(t *testing.T) {
	hostile := []string{
		"http://169.254.169.254/x",
		"httpx://a",
		"https://evil.com/a",
		"http://soundcloud.com/a/b",
		"file:///etc/passwd",
		"https://soundcloud.com.evil.com/a",
		"https://evilsoundcloud.com/a",
		"https://evil.com/a#soundcloud.com",
		"https://evil.com@soundcloud.com/a",
		"https://user:pass@soundcloud.com/a",
		"https://soundcloud.com:8080/a",
		"https://soundcloud.com/a\nhttps://evil.com/b",
		"  https://soundcloud.com/a",
	}

	for _, permalink := range hostile {
		t.Run(permalink, func(t *testing.T) {
			src := NewSource("soundcloud")
			got, err := src.Find(context.Background(), ports.FindRequest{
				Title:    "Song",
				Identity: identityWith("soundcloud", "999", permalink),
			})
			if err != nil {
				t.Fatalf("an unusable permalink is not an error, got %v", err)
			}
			if len(got) != 0 {
				t.Errorf("%q must not become a fetch target; got %+v", permalink, got)
			}
		})
	}
}

// TestFind_RejectsACatalogIDThatIsNotDigits pins the second half of the same
// class: the id is concatenated onto a fixed prefix, so anything but digits can
// rewrite the path or query of the URL rip is handed.
func TestFind_RejectsACatalogIDThatIsNotDigits(t *testing.T) {
	tests := []struct {
		service string
		id      string
	}{
		{"deezer", "1/../../x?y"},
		{"deezer", ""},
		{"deezer", "123\n"},
		{"deezer", "12 3"},
		{"tidal", "-1"},
		{"tidal", "1?q=2"},
		{"qobuz", "abc"},
		{"qobuz", "１２３"},
		{"qobuz", "https://evil.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.service+"/"+tt.id, func(t *testing.T) {
			src := NewSource(tt.service)
			got, err := src.Find(context.Background(), ports.FindRequest{
				Title:    "Song",
				Identity: identityWith(tt.service, tt.id, ""),
			})
			if err != nil {
				t.Fatalf("an unusable id is not an error, got %v", err)
			}
			if len(got) != 0 {
				t.Errorf("id %q must not become a fetch target; got %+v", tt.id, got)
			}
		})
	}
}

func TestFind_SoundCloudWithoutPermalinkYieldsNothing(t *testing.T) {
	src := NewSource("soundcloud")

	got, _ := src.Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("soundcloud", "999", ""),
	})
	if len(got) != 0 {
		t.Errorf("a numeric id is not a downloadable SoundCloud URL; want none, got %+v", got)
	}
}

func TestFind_NoMatchingProviderYieldsNothing(t *testing.T) {
	src := NewSource("tidal")

	got, err := src.Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("deezer", "123", ""),
	})
	if err != nil {
		t.Fatalf("an unresolvable track is not an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %d, want none", len(got))
	}
}

func TestName_NamespacesByService(t *testing.T) {
	if got := NewSource("tidal").Name(); got != "streamrip:tidal" {
		t.Errorf("Name() = %q", got)
	}
	if got := NewSource("deezer").Name(); got != "streamrip:deezer" {
		t.Errorf("Name() = %q", got)
	}
}

func TestSupported(t *testing.T) {
	for _, s := range []string{"tidal", "deezer", "qobuz", "soundcloud"} {
		if !Supported(s) {
			t.Errorf("Supported(%q) = false", s)
		}
	}
	if Supported("spotify") {
		t.Error("Supported(spotify) = true; streamrip cannot fetch Spotify audio")
	}
}

func TestLargestAudioFile_PicksBiggestAndIgnoresNonAudio(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "Artist", "Album")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, size int) string {
		path := filepath.Join(nested, name)
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("cover.jpg", 500_000)
	write("small.mp3", 20_000)
	want := write("full.flac", 900_000)

	got, err := largestAudioFile(dir)
	if err != nil {
		t.Fatalf("largestAudioFile: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLargestAudioFile_RejectsTinyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stub.mp3"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := largestAudioFile(dir); err == nil {
		t.Error("expected a too-small file to be rejected")
	}
}

// Issue #1976: rip has no size flag of its own, so an oversize file arrives
// whole and Fetch is the place that must refuse it. The error is what the
// download step turns into a RejectionDownload and a removed temp dir.
func TestFetch_RejectsAFileOverTheSizeCap(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rip")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSparseFile(t, filepath.Join(outDir, "set.flac"), maxFileSize+1)

	src := NewSource("tidal").WithBinary(bin)
	got, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: "https://tidal.com/browse/track/1"}, outDir)
	if err == nil {
		t.Fatalf("Fetch returned %q, want a file over the %d byte cap to be rejected", got, maxFileSize)
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %v, want it to name the size as the cause", err)
	}
}

// Issue #1983: rip exits non-zero both for a track its provider does not carry
// and for a provider that refused to serve us. Undistinguished, a throttled
// provider reaches the user as a track that could not be downloaded at all.
func TestFetch_ClassifiesAProviderThatRefusedToServe(t *testing.T) {
	tests := []struct {
		name            string
		stderr          string
		wantUnavailable bool
	}{
		{"throttled", "HTTP Error 429: Too Many Requests", true},
		{"nothing reached the provider", "ConnectionError: Connection refused", true},
		{"the provider answered and does not carry it", "Track not available in your region", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "rip")
			script := "#!/bin/sh\necho '" + tt.stderr + "' >&2\nexit 1\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			src := NewSource("tidal").WithBinary(bin)

			_, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: "https://tidal.com/browse/track/1"}, dir)

			if err == nil {
				t.Fatal("a rip that exited 1 reported no error")
			}
			if got := ports.IsSourceUnavailable(err); got != tt.wantUnavailable {
				t.Errorf("IsSourceUnavailable(%v) = %v, want %v", err, got, tt.wantUnavailable)
			}
		})
	}
}

// Issue #1983: a rip that is not installed is the source being unavailable, the
// one case where no output exists to classify on.
func TestFetch_MissingBinaryIsAnUnavailableSource(t *testing.T) {
	src := NewSource("tidal").WithBinary(filepath.Join(t.TempDir(), "rip-absent"))

	_, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: "https://tidal.com/browse/track/1"}, t.TempDir())

	if !ports.IsSourceUnavailable(err) {
		t.Errorf("Fetch error = %v, want a missing rip to read as an unavailable source", err)
	}
}

// writeSparseFile gives a file the requested size without writing its bytes, so
// a cap measured in hundreds of megabytes can be exercised in a unit test.
func writeSparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

func TestLargestAudioFile_NoAudioIsAnError(t *testing.T) {
	if _, err := largestAudioFile(t.TempDir()); err == nil {
		t.Error("expected an error when streamrip produced no audio")
	}
}

func TestWithBinary_IgnoresEmpty(t *testing.T) {
	src := NewSource("tidal").WithBinary("")
	if src.bin != defaultBin {
		t.Errorf("bin = %q, want the default %q", src.bin, defaultBin)
	}
	if got := NewSource("tidal").WithBinary("/usr/bin/rip").bin; got != "/usr/bin/rip" {
		t.Errorf("bin = %q", got)
	}
}

// TestFetch_TerminatesOptionsBeforeTheCandidateURL proves a candidate URL that
// looks like a flag reaches rip as a positional argument: the fake binary
// records its argv, and the URL must follow a "--" terminator.
func TestFetch_TerminatesOptionsBeforeTheCandidateURL(t *testing.T) {
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	bin := filepath.Join(dir, "rip")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argvFile + "\"\n" +
		"head -c 20000 /dev/zero > \"$2/track.flac\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const hostile = "--config-path=/tmp/evil.toml"
	src := NewSource("tidal").WithBinary(bin)
	if _, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: hostile}, outDir); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	n := len(argv)
	if n < 2 || argv[n-2] != "--" || argv[n-1] != hostile {
		t.Fatalf("argv = %q, want the candidate URL last and preceded by \"--\"", argv)
	}
}

func TestDiagnose_TurnsTracebacksIntoActionableCauses(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"database", "sqlite3.OperationalError: unable to open database file", "downloads_enabled"},
		{"quality", "NonStreamableError: Deezer HiFi is required for quality 2.", "lower [deezer] quality"},
		{"unknown", "KeyError: 'url'", "stderr: KeyError: 'url'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diagnose(tt.stderr); !strings.Contains(got, tt.want) {
				t.Errorf("diagnose(%q) = %q, want it to mention %q", tt.stderr, got, tt.want)
			}
		})
	}
}

// Issue #1973: an unclassified rip failure hands its stderr back inside the
// error, and rip prints its own config in a traceback — the Deezer arl is a
// session cookie, and the config path is host layout. The error is stored as a
// failure detail and logged, so neither may ride along.
func TestFetch_RedactsCredentialsRipPrintedInItsTraceback(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "rip")
	script := "#!/bin/sh\n" +
		"echo \"KeyError: 'url' while reading /home/ops/.config/streamrip/config.toml (arl=SECRET123)\" >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	src := NewSource("deezer").WithBinary(bin)

	_, err := src.Fetch(context.Background(), ports.AudioCandidate{URL: "https://www.deezer.com/track/1"}, dir)

	if err == nil {
		t.Fatal("a rip that exited 1 reported no error")
	}
	for _, secret := range []string{"SECRET123", "/home/ops"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error = %v, want %q masked", err, secret)
		}
	}
	if !strings.Contains(err.Error(), "KeyError") {
		t.Errorf("error = %v, want the diagnostic text kept for triage", err)
	}
}

// A traceback prints a config dict in Python's own repr, so the credential
// name arrives quoted and separated from its value by a colon.
func TestDiagnose_MasksCredentialsWhateverShapeTheTracebackPrintsThem(t *testing.T) {
	shapes := []string{
		"arl=SECRET123",
		"arl = SECRET123",
		"ARL=SECRET123",
		"{'arl': 'SECRET123'}",
		`{"arl": "SECRET123"}`,
		"Config(access_token='SECRET123')",
		"password: SECRET123",
	}

	for _, stderr := range shapes {
		t.Run(stderr, func(t *testing.T) {
			if got := diagnose("KeyError: " + stderr); strings.Contains(got, "SECRET123") {
				t.Errorf("diagnose(%q) = %q, want the credential masked", stderr, got)
			}
		})
	}
}

func TestAvailable_ProbesConfiguredBinary(t *testing.T) {
	present := filepath.Join(t.TempDir(), "rip")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake rip: %v", err)
	}
	if !NewSource("tidal").WithBinary(present).Available() {
		t.Errorf("Available() = false for existing binary %q", present)
	}
	missing := filepath.Join(t.TempDir(), "absent", "rip")
	if NewSource("tidal").WithBinary(missing).Available() {
		t.Errorf("Available() = true for missing binary %q", missing)
	}
}
