package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	QBitURL, QBitUsername, QBitPassword  string
	SonarrURL, SonarrAPIKey              string
	RadarrURL, RadarrAPIKey              string
	PollInterval                         time.Duration
	DangerousExtensions                  []string
	ActionMode                           string
	DryRun, DeleteData, PauseUnmapped    bool
	SonarrCategories, RadarrCategories   []string
	DatabasePath, ListenAddress          string
	SettingsPath, UIUsername, UIPassword string
	NeedsSetup                           bool
}

type Settings struct {
	QBitURL, QBitUsername, QBitPassword string
	SonarrURL, SonarrAPIKey             string
	RadarrURL, RadarrAPIKey             string
	PollIntervalSeconds                 int
	UIUsername, UIPassword              string
	PauseUnmapped                       *bool
}

var defaultDangerous = []string{
	".exe", ".scr", ".bat", ".cmd", ".com", ".msi", ".msp", ".ps1", ".psm1",
	".vbs", ".vbe", ".js", ".jse", ".wsf", ".wsh", ".hta", ".lnk", ".jar",
	".reg", ".dll", ".apk",
}

func Load() (Config, error) {
	seconds, err := intEnv("POLL_INTERVAL_SECONDS", 30)
	if err != nil || seconds < 1 {
		return Config{}, fmt.Errorf("POLL_INTERVAL_SECONDS must be a positive integer")
	}
	c := Config{
		QBitURL:             strings.TrimRight(os.Getenv("QBIT_URL"), "/"),
		QBitUsername:        os.Getenv("QBIT_USERNAME"),
		QBitPassword:        os.Getenv("QBIT_PASSWORD"),
		SonarrURL:           strings.TrimRight(os.Getenv("SONARR_URL"), "/"),
		SonarrAPIKey:        os.Getenv("SONARR_API_KEY"),
		RadarrURL:           strings.TrimRight(os.Getenv("RADARR_URL"), "/"),
		RadarrAPIKey:        os.Getenv("RADARR_API_KEY"),
		PollInterval:        time.Duration(seconds) * time.Second,
		DangerousExtensions: csvEnv("DANGEROUS_EXTENSIONS", defaultDangerous),
		ActionMode:          strings.ToLower(env("ACTION_MODE", "observe")),
		DryRun:              boolEnv("DRY_RUN", true),
		DeleteData:          boolEnv("DELETE_DATA", false),
		PauseUnmapped:       boolEnv("PAUSE_UNMAPPED", true),
		SonarrCategories:    csvEnv("SONARR_CATEGORIES", []string{"sonarr"}),
		RadarrCategories:    csvEnv("RADARR_CATEGORIES", []string{"radarr"}),
		DatabasePath:        env("DATABASE_PATH", "/data/torrentguard.db"),
		ListenAddress:       env("LISTEN_ADDRESS", ":8080"),
		SettingsPath:        env("SETTINGS_PATH", "/data/settings.json"),
		UIUsername:          env("UI_USERNAME", "admin"),
		UIPassword:          os.Getenv("UI_PASSWORD"),
	}
	if err := applySettings(&c); err != nil {
		return Config{}, fmt.Errorf("load settings: %w", err)
	}
	c.NeedsSetup = c.QBitURL == "" || c.QBitUsername == "" || c.UIPassword == ""
	if c.ActionMode != "observe" && c.ActionMode != "pause" && c.ActionMode != "delete" {
		return Config{}, fmt.Errorf("ACTION_MODE must be observe, pause, or delete")
	}
	return c, nil
}

func applySettings(c *Config) error {
	data, err := os.ReadFile(c.SettingsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s.QBitURL != "" {
		c.QBitURL = strings.TrimRight(s.QBitURL, "/")
	}
	if s.QBitUsername != "" {
		c.QBitUsername = s.QBitUsername
	}
	if s.QBitPassword != "" {
		c.QBitPassword = s.QBitPassword
	}
	if s.SonarrURL != "" {
		c.SonarrURL = strings.TrimRight(s.SonarrURL, "/")
	}
	if s.SonarrAPIKey != "" {
		c.SonarrAPIKey = s.SonarrAPIKey
	}
	if s.RadarrURL != "" {
		c.RadarrURL = strings.TrimRight(s.RadarrURL, "/")
	}
	if s.RadarrAPIKey != "" {
		c.RadarrAPIKey = s.RadarrAPIKey
	}
	if s.PollIntervalSeconds > 0 {
		c.PollInterval = time.Duration(s.PollIntervalSeconds) * time.Second
	}
	if s.UIUsername != "" {
		c.UIUsername = s.UIUsername
	}
	if s.UIPassword != "" {
		c.UIPassword = s.UIPassword
	}
	if s.PauseUnmapped != nil {
		c.PauseUnmapped = *s.PauseUnmapped
	}
	return nil
}

func SaveSettings(c Config, incoming Settings) error {
	if incoming.QBitPassword == "" {
		incoming.QBitPassword = c.QBitPassword
	}
	if incoming.SonarrAPIKey == "" {
		incoming.SonarrAPIKey = c.SonarrAPIKey
	}
	if incoming.RadarrAPIKey == "" {
		incoming.RadarrAPIKey = c.RadarrAPIKey
	}
	incoming.UIUsername = c.UIUsername
	incoming.UIPassword = c.UIPassword
	if incoming.PauseUnmapped == nil {
		incoming.PauseUnmapped = &c.PauseUnmapped
	}
	if incoming.PollIntervalSeconds < 1 {
		return fmt.Errorf("poll interval must be positive")
	}
	if incoming.QBitURL == "" || incoming.QBitUsername == "" {
		return fmt.Errorf("qBittorrent URL and username are required")
	}
	if incoming.QBitPassword == "" {
		return fmt.Errorf("qBittorrent password is required")
	}
	data, err := json.MarshalIndent(incoming, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.SettingsPath), 0750); err != nil {
		return err
	}
	temp := c.SettingsPath + ".tmp"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	return os.Rename(temp, c.SettingsPath)
}

func SaveInitialSettings(c Config, incoming Settings) error {
	incoming.UIUsername = strings.TrimSpace(incoming.UIUsername)
	if incoming.UIUsername == "" {
		incoming.UIUsername = "admin"
	}
	if incoming.UIPassword == "" {
		return fmt.Errorf("dashboard password is required")
	}
	if len(incoming.UIPassword) < 10 {
		return fmt.Errorf("dashboard password must be at least 10 characters")
	}
	if incoming.QBitURL == "" || incoming.QBitUsername == "" {
		return fmt.Errorf("qBittorrent URL and username are required")
	}
	if incoming.QBitPassword == "" {
		return fmt.Errorf("qBittorrent password is required")
	}
	if incoming.PollIntervalSeconds < 1 {
		incoming.PollIntervalSeconds = int(c.PollInterval.Seconds())
	}
	if incoming.PauseUnmapped == nil {
		incoming.PauseUnmapped = &c.PauseUnmapped
	}
	data, err := json.MarshalIndent(incoming, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.SettingsPath), 0750); err != nil {
		return err
	}
	temp := c.SettingsPath + ".setup.tmp"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	return os.Rename(temp, c.SettingsPath)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func intEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func csvEnv(key string, fallback []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(strings.ToLower(item)); item != "" {
			if key == "DANGEROUS_EXTENSIONS" && !strings.HasPrefix(item, ".") {
				item = "." + item
			}
			result = append(result, item)
		}
	}
	return result
}
