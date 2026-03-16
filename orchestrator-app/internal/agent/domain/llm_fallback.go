package domain

import (
	"context"
	"errors"
	"log/slog"
)

// FallbackClient wraps two LLMClient instances and falls back to the secondary
// when the primary returns a ProviderUnavailableError.
type FallbackClient struct {
	primary  LLMClient
	fallback LLMClient
}

// NewFallbackClient creates a FallbackClient that tries primary first,
// then fallback on provider unavailability.
func NewFallbackClient(primary, fallback LLMClient) *FallbackClient {
	return &FallbackClient{primary: primary, fallback: fallback}
}

// ChatCompletion tries the primary client; on ProviderUnavailableError, tries the fallback.
func (c *FallbackClient) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	resp, err := c.primary.ChatCompletion(ctx, req)
	if err == nil {
		return resp, nil
	}

	var unavail *ProviderUnavailableError
	if !errors.As(err, &unavail) {
		return ChatResponse{}, err // not a provider issue, don't fallback
	}

	slog.Warn("primary LLM unavailable, trying fallback",
		"provider", unavail.Provider, "error", unavail.Err)

	return c.fallback.ChatCompletion(ctx, req)
}
