package cli

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func TestResolveAPIURLWithAPIKeySkipsCachedState(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("DARI_CONFIG_DIR", configDir)
	t.Setenv("DARI_API_KEY", "dari_prod_key")
	if err := os.WriteFile(filepath.Join(configDir, state.Filename), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := (&globalFlags{}).resolveAPIURL()
	if err != nil {
		t.Fatal(err)
	}
	if got != api.DefaultAPIURL {
		t.Fatalf("resolveAPIURL() = %q, want %q", got, api.DefaultAPIURL)
	}
}

func TestResolveAPIURLWithAPIKeyKeepsFlagAndEnvPrecedence(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("DARI_CONFIG_DIR", configDir)
	t.Setenv("DARI_API_KEY", "dari_prod_key")
	if err := os.WriteFile(filepath.Join(configDir, state.Filename), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DARI_API_URL", "https://env.example.test/")
	got, err := (&globalFlags{}).resolveAPIURL()
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://env.example.test" {
		t.Fatalf("resolveAPIURL() with env = %q", got)
	}

	got, err = (&globalFlags{apiURL: "https://flag.example.test/"}).resolveAPIURL()
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://flag.example.test" {
		t.Fatalf("resolveAPIURL() with flag = %q", got)
	}
}

func TestOrgJWTRequestRejectsAPIKeyMode(t *testing.T) {
	t.Setenv("DARI_API_KEY", "dari_test")

	err := orgJWTRequest(&cobra.Command{}, &globalFlags{apiURL: "https://api.example.test"}, http.MethodGet, "/api-keys", nil, nil)
	if !errors.Is(err, auth.ErrNeedsUserLogin) {
		t.Fatalf("orgJWTRequest error = %v, want ErrNeedsUserLogin", err)
	}
}

func TestResolveAPIURLWithoutAPIKeyStillReadsCachedState(t *testing.T) {
	t.Setenv("DARI_API_KEY", "")
	configDir := t.TempDir()
	t.Setenv("DARI_CONFIG_DIR", configDir)
	if err := os.WriteFile(filepath.Join(configDir, state.Filename), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (&globalFlags{}).resolveAPIURL(); err == nil {
		t.Fatal("resolveAPIURL() succeeded with corrupt cached state and no API key")
	}
}
