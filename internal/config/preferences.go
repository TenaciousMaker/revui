// Package config owns user-wide revui preferences.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const Version = 1

// Preferences contains display choices that follow a user across repositories.
type Preferences struct {
	Version          int    `json:"version"`
	FileLayout       string `json:"file_layout"`
	FileScope        string `json:"file_scope"`
	WideFiles        bool   `json:"wide_files"`
	DiffView         string `json:"diff_view"`
	IgnoreWhitespace bool   `json:"ignore_whitespace,omitempty"`
	SemanticReflow   bool   `json:"semantic_reflow_experimental,omitempty"`
	NormalizedLayout bool   `json:"normalized_layout_experimental,omitempty"`
}

// Defaults returns a complete, valid preference set.
func Defaults() Preferences {
	return Preferences{Version: Version, FileLayout: "flat", FileScope: "changed", DiffView: "unified"}
}

// UserPath returns revui's preferences path in the operating system's user config directory.
func UserPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(configDir, "revui", "preferences.json"), nil
}

// Load reads preferences. Missing files return Defaults.
func Load(path string) (Preferences, error) {
	preferences, _, err := load(path)
	return preferences, err
}

// LoadWithFallback imports a legacy repository-local preference file only when
// the user-wide file does not exist yet.
func LoadWithFallback(path, fallbackPath string) (Preferences, error) {
	preferences, found, err := load(path)
	if err != nil || found || fallbackPath == "" {
		return preferences, err
	}
	legacy, legacyFound, err := load(fallbackPath)
	if err != nil || !legacyFound {
		return legacy, err
	}
	if err := Save(path, legacy); err != nil {
		return legacy, fmt.Errorf("migrate view preferences: %w", err)
	}
	return legacy, nil
}

func load(path string) (Preferences, bool, error) {
	preferences := Defaults()
	if path == "" {
		return preferences, false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return preferences, false, nil
	}
	if err != nil {
		return preferences, false, err
	}
	if err := json.Unmarshal(data, &preferences); err != nil {
		return Defaults(), true, fmt.Errorf("read view preferences: %w", err)
	}
	if preferences.Version != Version {
		return Defaults(), true, fmt.Errorf("unsupported preferences version %d", preferences.Version)
	}
	return preferences, true, nil
}

// Save atomically persists preferences with user-only permissions.
func Save(path string, preferences Preferences) error {
	if path == "" {
		return nil
	}
	preferences.Version = Version
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".preferences-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
