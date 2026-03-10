package common

import (
	"errors"
	"testing"
)

// Test new error types added during error handling unification
func TestNewErrorTypes(t *testing.T) {
	t.Run("NewRateLimitError", func(t *testing.T) {
		originalErr := errors.New("original error")
		err := NewRateLimitError("rate limit exceeded", originalErr)

		if err.Type != ErrorTypeRateLimit {
			t.Errorf("Expected error type %v, got %v", ErrorTypeRateLimit, err.Type)
		}
		if err.Message != "rate limit exceeded" {
			t.Errorf("Expected message 'rate limit exceeded', got '%s'", err.Message)
		}
		if err.Cause != originalErr {
			t.Errorf("Expected cause to be originalErr, got %v", err.Cause)
		}
	})

	t.Run("NewTimeoutError", func(t *testing.T) {
		originalErr := errors.New("timeout error")
		err := NewTimeoutError("request timeout", originalErr)

		if err.Type != ErrorTypeTimeout {
			t.Errorf("Expected error type %v, got %v", ErrorTypeTimeout, err.Type)
		}
		if err.Message != "request timeout" {
			t.Errorf("Expected message 'request timeout', got '%s'", err.Message)
		}
	})

	t.Run("NewNetworkError", func(t *testing.T) {
		err := NewNetworkError("network failure", nil)

		if err.Type != ErrorTypeNetworkError {
			t.Errorf("Expected error type %v, got %v", ErrorTypeNetworkError, err.Type)
		}
	})

	t.Run("NewResourceNotFoundError", func(t *testing.T) {
		err := NewResourceNotFoundError("resource not found")

		if err.Type != ErrorTypeResourceNotFound {
			t.Errorf("Expected error type %v, got %v", ErrorTypeResourceNotFound, err.Type)
		}
	})

	t.Run("NewInsufficientPermissionsError", func(t *testing.T) {
		err := NewInsufficientPermissionsError("access denied", nil)

		if err.Type != ErrorTypeInsufficientPermissions {
			t.Errorf("Expected error type %v, got %v", ErrorTypeInsufficientPermissions, err.Type)
		}
	})
}

func TestErrorTypeHelpers(t *testing.T) {
	t.Run("IsRateLimitError", func(t *testing.T) {
		rateLimitErr := NewRateLimitError("rate limit", nil)
		fetchErr := NewFetchError("fetch error", nil)

		if !IsRateLimitError(rateLimitErr) {
			t.Error("Expected IsRateLimitError to return true for rate limit error")
		}
		if IsRateLimitError(fetchErr) {
			t.Error("Expected IsRateLimitError to return false for fetch error")
		}
		if IsRateLimitError(errors.New("standard error")) {
			t.Error("Expected IsRateLimitError to return false for standard error")
		}
	})

	t.Run("IsTimeoutError", func(t *testing.T) {
		timeoutErr := NewTimeoutError("timeout", nil)
		authErr := NewAuthenticationError("auth error", nil)

		if !IsTimeoutError(timeoutErr) {
			t.Error("Expected IsTimeoutError to return true for timeout error")
		}
		if IsTimeoutError(authErr) {
			t.Error("Expected IsTimeoutError to return false for auth error")
		}
	})

	t.Run("IsNetworkError", func(t *testing.T) {
		networkErr := NewNetworkError("network error", nil)
		configErr := NewConfigError("config error", nil)

		if !IsNetworkError(networkErr) {
			t.Error("Expected IsNetworkError to return true for network error")
		}
		if IsNetworkError(configErr) {
			t.Error("Expected IsNetworkError to return false for config error")
		}
	})

	t.Run("IsResourceNotFoundError", func(t *testing.T) {
		notFoundErr := NewResourceNotFoundError("not found")
		ioErr := NewIOError("io error", nil)

		if !IsResourceNotFoundError(notFoundErr) {
			t.Error("Expected IsResourceNotFoundError to return true for not found error")
		}
		if IsResourceNotFoundError(ioErr) {
			t.Error("Expected IsResourceNotFoundError to return false for io error")
		}
	})

	t.Run("IsInsufficientPermissionsError", func(t *testing.T) {
		permErr := NewInsufficientPermissionsError("access denied", nil)
		validationErr := NewValidationError("validation error")

		if !IsInsufficientPermissionsError(permErr) {
			t.Error("Expected IsInsufficientPermissionsError to return true for permissions error")
		}
		if IsInsufficientPermissionsError(validationErr) {
			t.Error("Expected IsInsufficientPermissionsError to return false for validation error")
		}
	})
}

func TestUpdatedTypeStrings(t *testing.T) {
	tests := []struct {
		errorType ErrorType
		expected  string
	}{
		{ErrorTypeRateLimit, "rate_limit"},
		{ErrorTypeTimeout, "timeout"},
		{ErrorTypeNetworkError, "network"},
		{ErrorTypeResourceNotFound, "not_found"},
		{ErrorTypeInsufficientPermissions, "insufficient_permissions"},
	}

	for _, test := range tests {
		err := &ScorecardError{Type: test.errorType}
		actual := err.getTypeString()
		if actual != test.expected {
			t.Errorf("For error type %v, expected '%s', got '%s'",
				test.errorType, test.expected, actual)
		}
	}
}

func TestErrorLogging(t *testing.T) {
	t.Run("LogError with context", func(t *testing.T) {
		err := NewRateLimitError("API rate limit exceeded", nil).
			WithContext("endpoint", "/api/v1/repos").
			WithContext("remaining_requests", 0).
			WithContext("reset_time", "2025-08-02T15:04:05Z")

		// This test just ensures LogError doesn't panic
		// In a real test environment, you'd capture log output
		err.LogError()

		// Verify context was added
		if len(err.Context) != 3 {
			t.Errorf("Expected 3 context items, got %d", len(err.Context))
		}

		if err.Context["endpoint"] != "/api/v1/repos" {
			t.Errorf("Expected endpoint context, got %v", err.Context["endpoint"])
		}
	})
}
