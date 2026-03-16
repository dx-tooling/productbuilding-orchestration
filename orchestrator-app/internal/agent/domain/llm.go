package domain

import "context"

// LLMClient sends chat completion requests to an LLM API.
type LLMClient interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
