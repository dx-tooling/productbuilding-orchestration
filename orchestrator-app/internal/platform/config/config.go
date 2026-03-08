package config

import (
	"fmt"
	"log/slog"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv            string `env:"APP_ENV" envDefault:"development"`
	Port              string `env:"PORT" envDefault:"8080"`
	DatabasePath      string `env:"DATABASE_PATH" envDefault:"data/orchestrator.db"`
	PreviewDomain     string `env:"PREVIEW_DOMAIN" envDefault:"productbuilder.luminor-tech.net"`
	WorkspaceDir      string `env:"WORKSPACE_DIR" envDefault:"/opt/orchestrator/workspaces"`
	TargetsConfigPath string `env:"TARGETS_CONFIG_PATH" envDefault:"/opt/orchestrator/targets.json"`
	AWSRegion         string `env:"AWS_REGION" envDefault:"eu-central-1"`
}

func (c Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func (c Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

func Load() (Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	slog.Info("config loaded",
		"app_env", cfg.AppEnv,
		"port", cfg.Port,
		"preview_domain", cfg.PreviewDomain,
	)

	return cfg, nil
}
