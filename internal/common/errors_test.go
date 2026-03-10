package common

import (
	"errors"
	"testing"
)

func TestScorecardError_Error(t *testing.T) {
	tests := []struct {
		name     string
		error    *ScorecardError
		expected string
	}{
		{
			name: "error_with_cause",
			error: &ScorecardError{
				Type:    ErrorTypeFetch,
				Message: "failed to fetch data",
				Cause:   errors.New("network timeout"),
			},
			expected: "failed to fetch data: network timeout",
		},
		{
			name: "error_without_cause",
			error: &ScorecardError{
				Type:    ErrorTypeValidation,
				Message: "invalid input provided",
				Cause:   nil,
			},
			expected: "invalid input provided",
		},
		{
			name: "empty_message_with_cause",
			error: &ScorecardError{
				Type:    ErrorTypeConfig,
				Message: "",
				Cause:   errors.New("config missing"),
			},
			expected: ": config missing",
		},
		{
			name: "empty_message_without_cause",
			error: &ScorecardError{
				Type:    ErrorTypeIO,
				Message: "",
				Cause:   nil,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.error.Error()
			if result != tt.expected {
				t.Errorf("Error() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestScorecardError_Unwrap(t *testing.T) {
	tests := []struct {
		name          string
		error         *ScorecardError
		expectedCause error
	}{
		{
			name: "error_with_cause",
			error: &ScorecardError{
				Type:    ErrorTypeFetch,
				Message: "fetch failed",
				Cause:   errors.New("network error"),
			},
			expectedCause: errors.New("network error"),
		},
		{
			name: "error_without_cause",
			error: &ScorecardError{
				Type:    ErrorTypeValidation,
				Message: "validation failed",
				Cause:   nil,
			},
			expectedCause: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.error.Unwrap()
			if tt.expectedCause == nil {
				if result != nil {
					t.Errorf("Unwrap() = %v, want nil", result)
				}
			} else {
				if result == nil || result.Error() != tt.expectedCause.Error() {
					t.Errorf("Unwrap() = %v, want %v", result, tt.expectedCause)
				}
			}
		})
	}
}

func TestScorecardError_WithContext(t *testing.T) {
	tests := []struct {
		name        string
		error       *ScorecardError
		key         string
		value       interface{}
		wantContext map[string]interface{}
	}{
		{
			name: "add_context_to_empty_error",
			error: &ScorecardError{
				Type:    ErrorTypeFetch,
				Message: "fetch failed",
			},
			key:   "url",
			value: "https://api.github.com",
			wantContext: map[string]interface{}{
				"url": "https://api.github.com",
			},
		},
		{
			name: "add_context_to_existing_context",
			error: &ScorecardError{
				Type:    ErrorTypeValidation,
				Message: "validation failed",
				Context: map[string]interface{}{
					"field": "username",
				},
			},
			key:   "value",
			value: "invalid-user",
			wantContext: map[string]interface{}{
				"field": "username",
				"value": "invalid-user",
			},
		},
		{
			name: "overwrite_existing_context_key",
			error: &ScorecardError{
				Type:    ErrorTypeConfig,
				Message: "config error",
				Context: map[string]interface{}{
					"file": "old-config.yaml",
				},
			},
			key:   "file",
			value: "new-config.yaml",
			wantContext: map[string]interface{}{
				"file": "new-config.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.error.WithContext(tt.key, tt.value)

			// Should return the same error instance
			if result != tt.error {
				t.Error("WithContext() should return the same error instance")
			}

			// Check context values
			for key, expectedValue := range tt.wantContext {
				if actualValue, exists := result.Context[key]; !exists {
					t.Errorf("Context missing key %q", key)
				} else if actualValue != expectedValue {
					t.Errorf("Context[%q] = %v, want %v", key, actualValue, expectedValue)
				}
			}
		})
	}
}

func TestScorecardError_getTypeString(t *testing.T) {
	tests := []struct {
		name     string
		errType  ErrorType
		expected string
	}{
		{
			name:     "fetch_error_type",
			errType:  ErrorTypeFetch,
			expected: "fetch",
		},
		{
			name:     "validation_error_type",
			errType:  ErrorTypeValidation,
			expected: "validation",
		},
		{
			name:     "config_error_type",
			errType:  ErrorTypeConfig,
			expected: "config",
		},
		{
			name:     "io_error_type",
			errType:  ErrorTypeIO,
			expected: "io",
		},
		{
			name:     "authentication_error_type",
			errType:  ErrorTypeAuthentication,
			expected: "authentication",
		},
		{
			name:     "unknown_error_type",
			errType:  ErrorType(999),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ScorecardError{Type: tt.errType}
			result := err.getTypeString()
			if result != tt.expected {
				t.Errorf("getTypeString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestNewFetchError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		cause   error
	}{
		{
			name:    "fetch_error_with_cause",
			message: "failed to fetch repository",
			cause:   errors.New("HTTP 404 Not Found"),
		},
		{
			name:    "fetch_error_without_cause",
			message: "fetch operation failed",
			cause:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewFetchError(tt.message, tt.cause)

			if err.Type != ErrorTypeFetch {
				t.Errorf("Type = %v, want %v", err.Type, ErrorTypeFetch)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %q, want %q", err.Message, tt.message)
			}
			if err.Cause != tt.cause {
				t.Errorf("Cause = %v, want %v", err.Cause, tt.cause)
			}
			if err.Context == nil {
				t.Error("Context should be initialized")
			}
		})
	}
}

func TestNewValidationError(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "validation_error_message",
			message: "invalid PURL format",
		},
		{
			name:    "empty_validation_message",
			message: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewValidationError(tt.message)

			if err.Type != ErrorTypeValidation {
				t.Errorf("Type = %v, want %v", err.Type, ErrorTypeValidation)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %q, want %q", err.Message, tt.message)
			}
			if err.Cause != nil {
				t.Errorf("Cause = %v, want nil", err.Cause)
			}
			if err.Context == nil {
				t.Error("Context should be initialized")
			}
		})
	}
}

func TestNewConfigError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		cause   error
	}{
		{
			name:    "config_error_with_cause",
			message: "failed to load configuration",
			cause:   errors.New("file not found"),
		},
		{
			name:    "config_error_without_cause",
			message: "invalid configuration",
			cause:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewConfigError(tt.message, tt.cause)

			if err.Type != ErrorTypeConfig {
				t.Errorf("Type = %v, want %v", err.Type, ErrorTypeConfig)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %q, want %q", err.Message, tt.message)
			}
			if err.Cause != tt.cause {
				t.Errorf("Cause = %v, want %v", err.Cause, tt.cause)
			}
			if err.Context == nil {
				t.Error("Context should be initialized")
			}
		})
	}
}

func TestNewIOError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		cause   error
	}{
		{
			name:    "io_error_with_cause",
			message: "failed to write file",
			cause:   errors.New("disk full"),
		},
		{
			name:    "io_error_without_cause",
			message: "I/O operation failed",
			cause:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewIOError(tt.message, tt.cause)

			if err.Type != ErrorTypeIO {
				t.Errorf("Type = %v, want %v", err.Type, ErrorTypeIO)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %q, want %q", err.Message, tt.message)
			}
			if err.Cause != tt.cause {
				t.Errorf("Cause = %v, want %v", err.Cause, tt.cause)
			}
			if err.Context == nil {
				t.Error("Context should be initialized")
			}
		})
	}
}

func TestNewAuthenticationError(t *testing.T) {
	tests := []struct {
		name    string
		message string
		cause   error
	}{
		{
			name:    "authentication_error_with_cause",
			message: "authentication failed",
			cause:   errors.New("invalid token"),
		},
		{
			name:    "authentication_error_without_cause",
			message: "unauthorized access",
			cause:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAuthenticationError(tt.message, tt.cause)

			if err.Type != ErrorTypeAuthentication {
				t.Errorf("Type = %v, want %v", err.Type, ErrorTypeAuthentication)
			}
			if err.Message != tt.message {
				t.Errorf("Message = %q, want %q", err.Message, tt.message)
			}
			if err.Cause != tt.cause {
				t.Errorf("Cause = %v, want %v", err.Cause, tt.cause)
			}
			if err.Context == nil {
				t.Error("Context should be initialized")
			}
		})
	}
}

func TestIsAuthenticationError(t *testing.T) {
	tests := []struct {
		name     string
		error    error
		expected bool
	}{
		{
			name:     "authentication_scorecard_error",
			error:    NewAuthenticationError("auth failed", nil),
			expected: true,
		},
		{
			name:     "fetch_scorecard_error",
			error:    NewFetchError("fetch failed", nil),
			expected: false,
		},
		{
			name:     "validation_scorecard_error",
			error:    NewValidationError("validation failed"),
			expected: false,
		},
		{
			name:     "standard_go_error",
			error:    errors.New("standard error"),
			expected: false,
		},
		{
			name:     "nil_error",
			error:    nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAuthenticationError(tt.error)
			if result != tt.expected {
				t.Errorf("IsAuthenticationError() = %v, want %v", result, tt.expected)
			}
		})
	}
}
