package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Connection holds S3 connection parameters.
type Connection struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Region    string `json:"region"`
}

// AppConfig is the root config persisted to disk.
type AppConfig struct {
	Connections    []Connection `json:"connections"`
	LastConnection int          `json:"last_connection"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "s3browser")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads AppConfig from disk. Returns empty config on missing file.
func Load() (*AppConfig, error) {
	p, err := configPath()
	if err != nil {
		return &AppConfig{LastConnection: -1}, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &AppConfig{LastConnection: -1}, nil
		}
		return nil, err
	}
	cfg := &AppConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return &AppConfig{LastConnection: -1}, nil
	}
	return cfg, nil
}

// Save writes AppConfig to disk.
func (c *AppConfig) Save() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}
