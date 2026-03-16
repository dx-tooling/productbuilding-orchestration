package domain

import "fmt"

// ProviderUnavailableError indicates the LLM provider is temporarily unavailable
// after exhausting retries. This signals fallback logic to try an alternative.
type ProviderUnavailableError struct {
	Provider string
	Err      error
}

func (e *ProviderUnavailableError) Error() string {
	return fmt.Sprintf("provider %s unavailable: %v", e.Provider, e.Err)
}

func (e *ProviderUnavailableError) Unwrap() error {
	return e.Err
}
