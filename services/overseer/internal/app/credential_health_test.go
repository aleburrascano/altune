package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ownerHealthDir = flag.String("owner-health-dir", "", "serve one app, write its GET /api/health body into this dir and exit")

func TestMain(m *testing.M) {
	flag.Parse()
	if *ownerHealthDir == "" {
		os.Exit(m.Run())
	}
	if err := writeOwnerHealth(*ownerHealthDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func writeOwnerHealth(dir string) error {
	cfg := testConfig(filepath.Join(dir, "history.db"))
	r, err := launch(cfg, nil)
	if err != nil {
		return err
	}
	token, err := ownerToken(cfg)
	if err != nil {
		return errors.Join(err, r.stop(stopWait))
	}
	code, body, err := r.get("/api/health", token)
	if err = errors.Join(err, r.stop(stopWait)); err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET /api/health as the owner = %d %q; want 200", code, body)
	}
	return os.WriteFile(filepath.Join(dir, "owner-health.json"), body, 0o600)
}

type ownerHealth struct {
	Credential *struct {
		PasswordGrant bool `json:"passwordGrant"`
	} `json:"credential"`
}

func ownerHealthInAFreshProcess(t *testing.T, mode string, env ...string) ownerHealth {
	t.Helper()
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-owner-health-dir="+dir)
	child.Env = append(withoutOverseerEnv(os.Environ()), env...)
	out, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("the app in a fresh process failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "mode="+mode) {
		t.Fatalf("the fresh process did not select the %s token source:\n%s", mode, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "owner-health.json"))
	if err != nil {
		t.Fatalf("read the child's /api/health body: %v", err)
	}
	var h ownerHealth
	if err := json.Unmarshal(body, &h); err != nil {
		t.Fatalf("GET /api/health body %q: %v", body, err)
	}
	return h
}

func withoutOverseerEnv(environ []string) []string {
	var kept []string
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "OVERSEER_") {
			kept = append(kept, kv)
		}
	}
	return kept
}

func TestOwnerHealthReportsARefreshingCredentialsOwnState(t *testing.T) {
	h := ownerHealthInAFreshProcess(t, "refreshing",
		"OVERSEER_SUPABASE_URL=https://x.supabase.co",
		"OVERSEER_SUPABASE_ANON_KEY=anon-key",
		"OVERSEER_GOAPI_READONLY_REFRESH_TOKEN=seed-refresh",
		"OVERSEER_GOAPI_READONLY_EMAIL=readonly@altune.test",
		"OVERSEER_GOAPI_READONLY_PASSWORD=password",
		"OVERSEER_GOAPI_READONLY_REFRESH_TOKEN_FILE="+filepath.Join(t.TempDir(), "refresh_token"),
	)

	if h.Credential == nil {
		t.Fatal("a refreshing token source wired no credential health into GET /api/health")
	}
	if !h.Credential.PasswordGrant {
		t.Error("the credential health on GET /api/health is not the refreshing source's own state")
	}
}

func TestOwnerHealthOmitsCredentialsForAStaticToken(t *testing.T) {
	h := ownerHealthInAFreshProcess(t, "static", "OVERSEER_GOAPI_READONLY_TOKEN=static-jwt")

	if h.Credential != nil {
		t.Error("a static token has no refresh state to report, yet GET /api/health carries credential health")
	}
}
