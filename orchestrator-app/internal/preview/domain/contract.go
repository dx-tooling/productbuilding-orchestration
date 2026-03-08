package domain

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PreviewContract describes how to build and run a preview for a target repo.
type PreviewContract struct {
	Version  int             `yaml:"version"`
	Compose  ComposeConfig   `yaml:"compose"`
	Runtime  RuntimeConfig   `yaml:"runtime"`
	Database *DatabaseConfig `yaml:"database"`
}

type ComposeConfig struct {
	File    string `yaml:"file"`
	Service string `yaml:"service"`
}

type RuntimeConfig struct {
	InternalPort    int    `yaml:"internal_port"`
	HealthcheckPath string `yaml:"healthcheck_path"`
	StartupTimeout  int    `yaml:"startup_timeout_seconds"`
}

type DatabaseConfig struct {
	MigrateCommand string `yaml:"migrate_command"`
}

// ParseContract reads and validates the preview contract from a repo checkout.
func ParseContract(repoDir string) (*PreviewContract, error) {
	path := filepath.Join(repoDir, ".productbuilding", "preview", "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read preview contract: %w", err)
	}

	var c PreviewContract
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse preview contract: %w", err)
	}

	if c.Version != 1 {
		return nil, fmt.Errorf("unsupported preview contract version: %d", c.Version)
	}
	if c.Compose.File == "" {
		return nil, fmt.Errorf("preview contract: compose.file is required")
	}
	if c.Compose.Service == "" {
		return nil, fmt.Errorf("preview contract: compose.service is required")
	}
	if c.Runtime.InternalPort == 0 {
		c.Runtime.InternalPort = 8080
	}
	if c.Runtime.HealthcheckPath == "" {
		c.Runtime.HealthcheckPath = "/healthz"
	}
	if c.Runtime.StartupTimeout == 0 {
		c.Runtime.StartupTimeout = 300
	}

	return &c, nil
}
