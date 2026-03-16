package domain

import (
	"testing"
)

func TestNewLLMClient_Anthropic(t *testing.T) {
	client, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:   ProviderAnthropic,
			APIKey: "test-key",
			Model:  "claude-opus-4-6",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.(*AnthropicClient); !ok {
		t.Errorf("expected *AnthropicClient, got %T", client)
	}
}

func TestNewLLMClient_OpenAICompat(t *testing.T) {
	client, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:    ProviderOpenAICompat,
			APIKey:  "test-key",
			Model:   "gpt-4o",
			BaseURL: "https://api.openai.com/v1",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.(*OpenAICompatClient); !ok {
		t.Errorf("expected *OpenAICompatClient, got %T", client)
	}
}

func TestNewLLMClient_WithFallback(t *testing.T) {
	client, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:   ProviderAnthropic,
			APIKey: "test-key",
			Model:  "claude-opus-4-6",
		},
		Fallback: &ProviderConfig{
			Type:    ProviderOpenAICompat,
			APIKey:  "test-key-2",
			Model:   "gpt-4o",
			BaseURL: "https://api.openai.com/v1",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.(*FallbackClient); !ok {
		t.Errorf("expected *FallbackClient, got %T", client)
	}
}

func TestNewLLMClient_MissingAPIKey(t *testing.T) {
	_, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:  ProviderAnthropic,
			Model: "claude-opus-4-6",
		},
	})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestNewLLMClient_MissingModel(t *testing.T) {
	_, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:   ProviderAnthropic,
			APIKey: "test-key",
		},
	})
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestNewLLMClient_OpenAICompat_MissingBaseURL(t *testing.T) {
	_, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:   ProviderOpenAICompat,
			APIKey: "test-key",
			Model:  "gpt-4o",
		},
	})
	if err == nil {
		t.Fatal("expected error for missing base URL")
	}
}

func TestNewLLMClient_UnknownProvider(t *testing.T) {
	_, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:   "unknown",
			APIKey: "test-key",
			Model:  "some-model",
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewLLMClient_FallbackProviderError(t *testing.T) {
	_, err := NewLLMClient(LLMConfig{
		Primary: ProviderConfig{
			Type:   ProviderAnthropic,
			APIKey: "test-key",
			Model:  "claude-opus-4-6",
		},
		Fallback: &ProviderConfig{
			Type: ProviderOpenAICompat,
			// Missing API key
			Model:   "gpt-4o",
			BaseURL: "https://api.openai.com/v1",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid fallback config")
	}
}
