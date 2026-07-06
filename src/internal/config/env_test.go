package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv_SetsUnsetVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	contents := "TESTENV_HOST=broker.local\n# a comment\n\nTESTENV_PORT=\"1883\"\nTESTENV_USER='bob'\nMALFORMED_LINE_NO_EQUALS\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"TESTENV_HOST", "TESTENV_PORT", "TESTENV_USER"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("TESTENV_HOST"); got != "broker.local" {
		t.Errorf("TESTENV_HOST = %q, want broker.local", got)
	}
	if got := os.Getenv("TESTENV_PORT"); got != "1883" {
		t.Errorf("TESTENV_PORT = %q, want 1883 (quotes should be stripped)", got)
	}
	if got := os.Getenv("TESTENV_USER"); got != "bob" {
		t.Errorf("TESTENV_USER = %q, want bob (quotes should be stripped)", got)
	}
}

func TestLoadDotEnv_DoesNotOverrideExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("TESTENV_OVERRIDE=fromfile\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TESTENV_OVERRIDE", "fromenv")

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}
	if got := os.Getenv("TESTENV_OVERRIDE"); got != "fromenv" {
		t.Errorf("TESTENV_OVERRIDE = %q, want fromenv (existing env must win)", got)
	}
}

func TestLoadDotEnv_MissingFileIsNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.env")
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("missing .env should not be an error, got %v", err)
	}
}
