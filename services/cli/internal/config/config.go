package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// File is the on-disk config written by `turbo-cache login` and read by
// every other command. Only two fields exist today — YAGNI on anything else.
//
// Decision: JSON via stdlib encoding/json, not YAML — a 2-field config file
// doesn't earn a new dependency (gopkg.in/yaml.v3) when the stdlib already
// does this in one import.
type File struct {
	APIURL string `json:"api_url,omitempty"`
	Token  string `json:"token,omitempty"` // OIDC access token (JWT) from `login` — NOT the hashed cache token
}

// Path returns the config file location, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "turbo-cache", "config.json"), nil
}

// Load reads the config file. A missing file is not an error — it returns a
// zero-value File so first-run commands (e.g. `login`) work.
func Load() (File, error) {
	p, err := Path()
	if err != nil {
		return File{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

// Save writes the config file with 0600 permissions — it holds a bearer token.
func Save(f File) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Pick resolves precedence: flag > env > file. An empty string means
// "not set" at that level.
func Pick(flagVal, envVal, fileVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if envVal != "" {
		return envVal
	}
	return fileVal
}
