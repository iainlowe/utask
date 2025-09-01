package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

type Config struct {
	NATS struct {
		URL string `yaml:"url"`
	} `yaml:"nats"`
	OpenAI struct {
		APIKey string `yaml:"api_key"`
		Model  string `yaml:"model"`
	} `yaml:"openai"`
	UI struct {
		Profile string `yaml:"profile"`
	} `yaml:"ui"`
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".utask", "config.yaml"), nil
}

func LoadFromFile(path string) (*Config, error) {
	cfg := &Config{}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	return cfg, nil
}

// OverlayEnv applies environment variables onto cfg.
func OverlayEnv(cfg *Config) {
	if v := os.Getenv("UTASK_NATS_URL"); v != "" {
		cfg.NATS.URL = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAI.APIKey = v
	}
	if v := os.Getenv("UTASK_OPENAI_MODEL"); v != "" {
		cfg.OpenAI.Model = v
	}
	if v := os.Getenv("UTASK_PROFILE"); v != "" {
		cfg.UI.Profile = v
	}
}
