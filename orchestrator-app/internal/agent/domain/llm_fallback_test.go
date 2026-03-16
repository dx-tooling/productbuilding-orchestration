package domain

import (
	"context"
	"errors"
	"testing"
)

func TestFallbackClient_PrimarySucceeds(t *testing.T) {
	primary := &mockLLMClient{
		responses: []ChatResponse{{Content: "primary response", FinishReason: "stop"}},
	}
	fallback := &mockLLMClient{
		responses: []ChatResponse{{Content: "fallback response", FinishReason: "stop"}},
	}
	client := NewFallbackClient(primary, fallback)

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "primary response" {
		t.Errorf("expected primary response, got %q", resp.Content)
	}
	if fallback.callIdx != 0 {
		t.Errorf("expected fallback not called, got %d calls", fallback.callIdx)
	}
}

func TestFallbackClient_FallsBackOnProviderUnavailable(t *testing.T) {
	primary := &mockLLMClient{
		errors: []error{&ProviderUnavailableError{Provider: "anthropic", Err: errors.New("502")}},
	}
	fallback := &mockLLMClient{
		responses: []ChatResponse{{Content: "fallback response", FinishReason: "stop"}},
	}
	client := NewFallbackClient(primary, fallback)

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "fallback response" {
		t.Errorf("expected fallback response, got %q", resp.Content)
	}
}

func TestFallbackClient_DoesNotFallbackOnOtherErrors(t *testing.T) {
	primary := &mockLLMClient{
		errors: []error{errors.New("bad request: invalid model")},
	}
	fallback := &mockLLMClient{
		responses: []ChatResponse{{Content: "fallback response", FinishReason: "stop"}},
	}
	client := NewFallbackClient(primary, fallback)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if fallback.callIdx != 0 {
		t.Errorf("expected fallback not called for non-provider error, got %d calls", fallback.callIdx)
	}
}

func TestFallbackClient_BothFail(t *testing.T) {
	primary := &mockLLMClient{
		errors: []error{&ProviderUnavailableError{Provider: "anthropic", Err: errors.New("down")}},
	}
	fallback := &mockLLMClient{
		errors: []error{&ProviderUnavailableError{Provider: "openaicompat", Err: errors.New("also down")}},
	}
	client := NewFallbackClient(primary, fallback)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error when both providers fail")
	}
	var unavail *ProviderUnavailableError
	if !errors.As(err, &unavail) {
		t.Errorf("expected ProviderUnavailableError from fallback, got %T: %v", err, err)
	}
}
