package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const stateVersion = 2

type QuarantineRecord struct {
	Reason         string    `json:"reason"`
	Since          time.Time `json:"since"`
	AwaitingHealth bool      `json:"awaitingHealth,omitempty"`
}

type KnownDevice struct {
	Path     string    `json:"path"`
	LastSeen time.Time `json:"lastSeen"`
}

type persistedState struct {
	Version     int                         `json:"version"`
	Claims      map[string]PreparedClaim    `json:"claims"`
	Quarantined map[string]QuarantineRecord `json:"quarantined"`
	Known       map[string]KnownDevice      `json:"known"`
}

func (m *Manager) load() error {
	data, err := os.ReadFile(filepath.Join(m.config.StateDir, "claims.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &m.state); err != nil {
		return fmt.Errorf("decode claim state: %w", err)
	}
	if m.state.Version == 1 {
		m.state.Version = stateVersion
	}
	if m.state.Version != stateVersion || m.state.Claims == nil {
		return fmt.Errorf("unsupported claim state version %d", m.state.Version)
	}
	if m.state.Quarantined == nil {
		m.state.Quarantined = map[string]QuarantineRecord{}
	}
	if m.state.Known == nil {
		m.state.Known = map[string]KnownDevice{}
	}
	return nil
}

func (m *Manager) persist() error {
	if err := os.MkdirAll(m.config.StateDir, 0o755); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(m.config.StateDir, "claims.json"), m.state, 0o600)
}

func atomicJSON(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
