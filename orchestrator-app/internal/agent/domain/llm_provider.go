package domain

import "fmt"

// ProviderType identifies an LLM provider backend.
type ProviderType string

const (
	ProviderAnthropic    ProviderType = "anthropic"
	ProviderOpenAICompat ProviderType = "openaicompat"
)

// ProviderConfig holds the configuration for a single LLM provider.
type ProviderConfig struct {
	Type    ProviderType
	APIKey  string
	Model   string
	BaseURL string // required for openaicompat, ignored for anthropic
}

// LLMConfig holds the full LLM configuration including optional fallback.
type LLMConfig struct {
	Primary  ProviderConfig
	Fallback *ProviderConfig // nil = no fallback
	Retry    RetryConfig
}

// NewLLMClient builds an LLMClient from config, optionally wrapping in FallbackClient.
func NewLLMClient(cfg LLMConfig) (LLMClient, error) {
	if cfg.Retry.MaxRetries == 0 && cfg.Retry.BaseDelay == 0 {
		cfg.Retry = DefaultRetryConfig()
	}

	primary, err := newProvider(cfg.Primary, cfg.Retry)
	if err != nil {
		return nil, fmt.Errorf("primary provider: %w", err)
	}

	if cfg.Fallback == nil {
		return primary, nil
	}

	fallback, err := newProvider(*cfg.Fallback, cfg.Retry)
	if err != nil {
		return nil, fmt.Errorf("fallback provider: %w", err)
	}

	return NewFallbackClient(primary, fallback), nil
}

func newProvider(cfg ProviderConfig, retry RetryConfig) (LLMClient, error) {
	switch cfg.Type {
	case ProviderAnthropic:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("anthropic provider requires API key")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("anthropic provider requires model")
		}
		return NewAnthropicClient(cfg.APIKey, cfg.Model), nil

	case ProviderOpenAICompat:
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("openaicompat provider requires API key")
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("openaicompat provider requires model")
		}
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("openaicompat provider requires base URL")
		}
		return NewOpenAICompatClientWithConfig(cfg.APIKey, cfg.Model, cfg.BaseURL, retry), nil

	default:
		return nil, fmt.Errorf("unknown provider type: %q", cfg.Type)
	}
}
