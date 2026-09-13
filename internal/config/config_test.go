package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func cleanConfigEnv(t *testing.T, settingsPath string) {
	t.Helper()
	for _, key := range []string{
		"QBIT_URL", "QBIT_USERNAME", "QBIT_PASSWORD", "QBIT_API_KEY", "QBIT_AUTH_MODE",
		"SONARR_URL", "SONARR_API_KEY",
		"RADARR_URL", "RADARR_API_KEY", "UI_USERNAME", "UI_PASSWORD",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("SETTINGS_PATH", settingsPath)
	t.Setenv("DATABASE_PATH", filepath.Join(filepath.Dir(settingsPath), "guard.db"))
}

func TestLoadAllowsUnconfiguredFirstStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	cleanConfigEnv(t, path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if !cfg.NeedsSetup {
		t.Fatal("expected first start to require setup")
	}
}

func TestSaveInitialSettingsCompletesSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	cleanConfigEnv(t, path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	pause := false
	err = SaveInitialSettings(cfg, Settings{
		QBitURL: "http://nas:8080", QBitUsername: "rune", QBitPassword: "qbit-secret",
		UIUsername: "owner", UIPassword: "a-long-password",
		PollIntervalSeconds: 15, PauseUnmapped: &pause,
	})
	if err != nil {
		t.Fatalf("SaveInitialSettings returned an error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NeedsSetup {
		t.Fatal("expected setup to be complete")
	}
	if loaded.UIUsername != "owner" || loaded.UIPassword != "a-long-password" {
		t.Fatal("administrator credentials were not loaded")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("settings file is too permissive: %v", info.Mode().Perm())
	}
}

func TestSaveInitialSettingsRequiresCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	cleanConfigEnv(t, path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveInitialSettings(cfg, Settings{QBitURL: "http://nas", QBitUsername: "user"}); err == nil {
		t.Fatal("expected missing passwords to be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid setup must not create a settings file")
	}
}

func TestSaveInitialSettingsAcceptsAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	cleanConfigEnv(t, path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	err = SaveInitialSettings(cfg, Settings{
		QBitURL: "http://nas:8080", QBitAuthMode: "api_key", QBitAPIKey: "qbt_example",
		UIUsername: "owner", UIPassword: "a-long-password", PollIntervalSeconds: 30,
	})
	if err != nil {
		t.Fatalf("API key setup was rejected: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NeedsSetup || loaded.QBitAuthMode != "api_key" || loaded.QBitAPIKey != "qbt_example" {
		t.Fatalf("API key setup was not loaded: %+v", loaded)
	}
}
