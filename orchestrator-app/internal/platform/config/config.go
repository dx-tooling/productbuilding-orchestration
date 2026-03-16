package config

import (
	"fmt"
	"log/slog"

	agentdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv             string `env:"APP_ENV" envDefault:"development"`
	Port               string `env:"PORT" envDefault:"8080"`
	DatabasePath       string `env:"DATABASE_PATH" envDefault:"data/orchestrator.db"`
	PreviewDomain      string `env:"PREVIEW_DOMAIN" envDefault:""`
	WorkspaceDir       string `env:"WORKSPACE_DIR" envDefault:"/opt/orchestrator/workspaces"`
	TargetsConfigPath  string `env:"TARGETS_CONFIG_PATH" envDefault:"/opt/orchestrator/targets.json"`
	AWSRegion          string `env:"AWS_REGION" envDefault:"eu-central-1"`
	SlackSigningSecret string `env:"SLACK_SIGNING_SECRET"`
	SlackChannelPrefix string `env:"SLACK_CHANNEL_PREFIX" envDefault:"productbuilding-"`
	AcmeEmail          string `env:"ACME_EMAIL" envDefault:"admin@example.com"`
	SlackWorkspace     string `env:"SLACK_WORKSPACE"` // Slack workspace subdomain (e.g. "myteam")

	// LLM configuration
	LLMProvider         string `env:"LLM_PROVIDER" envDefault:"anthropic"`
	LLMModel            string `env:"LLM_MODEL" envDefault:"claude-opus-4-6"`
	LLMApiKey           string `env:"LLM_API_KEY"`
	LLMBaseURL          string `env:"LLM_BASE_URL"`          // required for openaicompat
	LLMFallbackProvider string `env:"LLM_FALLBACK_PROVIDER"` // optional
	LLMFallbackModel    string `env:"LLM_FALLBACK_MODEL"`    // optional
	LLMFallbackApiKey   string `env:"LLM_FALLBACK_API_KEY"`  // optional
	LLMFallbackBaseURL  string `env:"LLM_FALLBACK_BASE_URL"` // optional

	LLMRequestTimeout int `env:"LLM_REQUEST_TIMEOUT_SECS" envDefault:"60"`
	LLMMaxRetries     int `env:"LLM_MAX_RETRIES" envDefault:"3"`
	AgentRunTimeout   int `env:"AGENT_RUN_TIMEOUT_SECS" envDefault:"120"`
	AgentTokenBudget  int `env:"AGENT_TOKEN_BUDGET" envDefault:"8000"`
}

func (c Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func (c Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

// LLMConfig builds the agent domain LLMConfig from environment config.
func (c Config) LLMConfig() agentdomain.LLMConfig {
	cfg := agentdomain.LLMConfig{
		Primary: agentdomain.ProviderConfig{
			Type:    agentdomain.ProviderType(c.LLMProvider),
			APIKey:  c.LLMApiKey,
			Model:   c.LLMModel,
			BaseURL: c.LLMBaseURL,
		},
	}

	if c.LLMFallbackProvider != "" {
		cfg.Fallback = &agentdomain.ProviderConfig{
			Type:    agentdomain.ProviderType(c.LLMFallbackProvider),
			APIKey:  c.LLMFallbackApiKey,
			Model:   c.LLMFallbackModel,
			BaseURL: c.LLMFallbackBaseURL,
		}
	}

	return cfg
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
