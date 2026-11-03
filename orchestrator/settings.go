package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// settings are the forge-wide knobs the UI can change live. .env gives the defaults; the UI saves its changes to
// SETTINGS_FILE, which wins over .env on the next start.
type settings struct {
	// MaxAgents caps the specs and US worked on at once, each running one agent at a time.
	MaxAgents int `json:"maxAgents"`
	// MaxCostPerSpec stops a spec's remaining stories once its agents have cost more (USD, as reported by Claude Code).
	MaxCostPerSpec float64 `json:"maxCostPerSpec"`
	PollSeconds    int     `json:"pollSeconds"`
}

func (s settings) validate() error {
	switch {
	case s.MaxAgents < 1 || s.MaxAgents > 20:
		return errors.New("max agents must be between 1 and 20")
	case s.MaxCostPerSpec <= 0 || s.MaxCostPerSpec > 1000:
		return errors.New("the default budget must be between $0 and $1000")
	case s.PollSeconds < 1 || s.PollSeconds > 3600: // below 1 s, the poll loop and waitCI would hammer Gitea
		return errors.New("the poll interval must be between 1 and 3600 seconds")
	}
	return nil
}

func (s settings) pollEvery() time.Duration { return time.Duration(s.PollSeconds) * time.Second }

// loadSettings returns the saved settings, or the defaults when nothing was saved yet.
func loadSettings(path string, defaults settings) (settings, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, err
	}
	s := defaults // a field missing from an older file keeps its default
	if err := json.Unmarshal(data, &s); err != nil {
		return defaults, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return defaults, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// saveSettings writes through a temporary file, so a crash never leaves a half-written one.
func saveSettings(path string, s settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// settings returns a copy of the live settings.
func (f *forge) settings() settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}

// updateSettings validates, saves, then applies: a refused or unsaved change changes nothing.
// A lower MaxAgents never stops running agents; it only holds back new ones.
func (f *forge) updateSettings(s settings) error {
	if err := s.validate(); err != nil {
		return fmt.Errorf("%w: %w", err, errBadRequest)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := saveSettings(f.cfg.SettingsFile, s); err != nil {
		return err
	}
	f.live = s
	return nil
}
