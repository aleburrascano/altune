package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsEachKeyLiterallyAndKeepsTheShellsValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "# comment\nPS_TEST_PLAIN=a (b) c\nPS_TEST_QUOTED=\"q v\"\nPS_TEST_SINGLE='s'\nPS_TEST_SHELL=from file\nnot a setting\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"PS_TEST_PLAIN", "PS_TEST_QUOTED", "PS_TEST_SINGLE"} {
		t.Setenv(name, "")
	}
	t.Setenv("PS_TEST_SHELL", "from shell")
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PS_TEST_PLAIN": "a (b) c", "PS_TEST_QUOTED": "q v", "PS_TEST_SINGLE": "s", "PS_TEST_SHELL": "from shell"}
	for name, value := range want {
		if got := os.Getenv(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestLoadEnvFileFailsOnAMissingFile(t *testing.T) {
	if err := loadEnvFile(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Fatal("want an error")
	}
}

func TestEnvKeyFailsLoudlyWhenUnset(t *testing.T) {
	t.Setenv("PROVIDERSCHEMA_TEST_KEY", "")
	if _, err := envKey("PROVIDERSCHEMA_TEST_KEY"); err == nil {
		t.Fatal("want an error")
	}
	t.Setenv("PROVIDERSCHEMA_TEST_KEY", "v")
	if got, err := envKey("PROVIDERSCHEMA_TEST_KEY"); err != nil || got != "v" {
		t.Fatalf("got %q, %v", got, err)
	}
}
