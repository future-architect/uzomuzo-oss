package common

import (
	"errors"
	"fmt"
	"log/slog"
)

// ScorecardError represents different types of errors in the scorecard application
type ScorecardError struct {
	Type    ErrorType
	Message string
	Cause   error
	Context map[string]interface{}
}

// ErrorType represents the category of error
type ErrorType int

const (
	ErrorTypeFetch ErrorType = iota
	ErrorTypeValidation
	ErrorTypeConfig
	ErrorTypeIO
	ErrorTypeAuthentication
	ErrorTypeRateLimit
	ErrorTypeTimeout
	ErrorTypeNetworkError
	ErrorTypeResourceNotFound
	ErrorTypeInsufficientPermissions
)

// Error implements the error interface
func (e *ScorecardError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying error
func (e *ScorecardError) Unwrap() error {
	return e.Cause
}

// WithContext adds context information to the error
func (e *ScorecardError) WithContext(key string, value interface{}) *ScorecardError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// LogError logs the error with structured logging
func (e *ScorecardError) LogError() {
	args := make([]any, 0, len(e.Context)*2+4)
	args = append(args, "error_type", e.getTypeString())
	args = append(args, "message", e.Message)

	for k, v := range e.Context {
		args = append(args, k, v)
	}

	if e.Cause != nil {
		args = append(args, "cause", e.Cause)
	}

	slog.Error("Scorecard error occurred", args...)
}

func (e *ScorecardError) getTypeString() string {
	switch e.Type {
	case ErrorTypeFetch:
		return "fetch"
	case ErrorTypeValidation:
		return "validation"
	case ErrorTypeConfig:
		return "config"
	case ErrorTypeIO:
		return "io"
	case ErrorTypeAuthentication:
		return "authentication"
	case ErrorTypeRateLimit:
		return "rate_limit"
	case ErrorTypeTimeout:
		return "timeout"
	case ErrorTypeNetworkError:
		return "network"
	case ErrorTypeResourceNotFound:
		return "not_found"
	case ErrorTypeInsufficientPermissions:
		return "insufficient_permissions"
	default:
		return "unknown"
	}
}

// NewFetchError creates a new fetch error
func NewFetchError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeFetch,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// NewValidationError creates a new validation error
func NewValidationError(message string) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeValidation,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// NewConfigError creates a new configuration error
func NewConfigError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeConfig,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// NewIOError creates a new I/O error
func NewIOError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeIO,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// NewAuthenticationError creates a new authentication error
func NewAuthenticationError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeAuthentication,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// IsAuthenticationError checks if an error is an authentication error
func IsAuthenticationError(err error) bool {
	var scorecardErr *ScorecardError
	if errors.As(err, &scorecardErr) {
		return scorecardErr.Type == ErrorTypeAuthentication
	}
	return false
}

// NewRateLimitError creates a new rate limit error
func NewRateLimitError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeRateLimit,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// NewTimeoutError creates a new timeout error
func NewTimeoutError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeTimeout,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// NewNetworkError creates a new network error
func NewNetworkError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeNetworkError,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// NewResourceNotFoundError creates a new resource not found error
func NewResourceNotFoundError(message string) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeResourceNotFound,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// NewInsufficientPermissionsError creates a new insufficient permissions error
func NewInsufficientPermissionsError(message string, cause error) *ScorecardError {
	return &ScorecardError{
		Type:    ErrorTypeInsufficientPermissions,
		Message: message,
		Cause:   cause,
		Context: make(map[string]interface{}),
	}
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	var scorecardErr *ScorecardError
	if errors.As(err, &scorecardErr) {
		return scorecardErr.Type == ErrorTypeRateLimit
	}
	return false
}

// IsTimeoutError checks if an error is a timeout error
func IsTimeoutError(err error) bool {
	var scorecardErr *ScorecardError
	if errors.As(err, &scorecardErr) {
		return scorecardErr.Type == ErrorTypeTimeout
	}
	return false
}

// IsNetworkError checks if an error is a network error
func IsNetworkError(err error) bool {
	var scorecardErr *ScorecardError
	if errors.As(err, &scorecardErr) {
		return scorecardErr.Type == ErrorTypeNetworkError
	}
	return false
}

// IsResourceNotFoundError checks if an error is a resource not found error
func IsResourceNotFoundError(err error) bool {
	var scorecardErr *ScorecardError
	if errors.As(err, &scorecardErr) {
		return scorecardErr.Type == ErrorTypeResourceNotFound
	}
	return false
}

// IsInsufficientPermissionsError checks if an error is an insufficient permissions error
func IsInsufficientPermissionsError(err error) bool {
	var scorecardErr *ScorecardError
	if errors.As(err, &scorecardErr) {
		return scorecardErr.Type == ErrorTypeInsufficientPermissions
	}
	return false
}
