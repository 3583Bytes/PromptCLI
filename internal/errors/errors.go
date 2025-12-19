package errors

import (
	"fmt"
	"strings"
)

// ErrorType represents the category of error
type ErrorType string

const (
	ErrorTypeNetwork    ErrorType = "network_error"
	ErrorTypeValidation ErrorType = "validation_error"
	ErrorTypeIO         ErrorType = "io_error"
	ErrorTypeParse      ErrorType = "parse_error"
	ErrorTypePermission ErrorType = "permission_error"
	ErrorTypeTimeout    ErrorType = "timeout_error"
	ErrorTypeUnknown    ErrorType = "unknown_error"
)

// AppError represents a structured application error
type AppError struct {
	Type    ErrorType
	Message string
	Cause   error
	Context map[string]interface{}
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (caused by: %v)", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

// New creates a new AppError
func New(errorType ErrorType, message string, context map[string]interface{}) *AppError {
	return &AppError{
		Type:    errorType,
		Message: message,
		Context: context,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(errorType ErrorType, message string, err error, context map[string]interface{}) *AppError {
	return &AppError{
		Type:    errorType,
		Message: message,
		Cause:   err,
		Context: context,
	}
}

// NetworkError creates a network-related error
func NetworkError(message string, err error) *AppError {
	return Wrap(ErrorTypeNetwork, message, err, nil)
}

// ValidationError creates a validation error
func ValidationError(message string, context map[string]interface{}) *AppError {
	return New(ErrorTypeValidation, message, context)
}

// IOError creates an I/O error
func IOError(message string, err error) *AppError {
	return Wrap(ErrorTypeIO, message, err, nil)
}

// ParseError creates a parsing error
func ParseError(message string, err error) *AppError {
	return Wrap(ErrorTypeParse, message, err, nil)
}

// PermissionError creates a permission error
func PermissionError(message string) *AppError {
	return New(ErrorTypePermission, message, nil)
}

// TimeoutError creates a timeout error
func TimeoutError(message string, err error) *AppError {
	return Wrap(ErrorTypeTimeout, message, err, nil)
}

// As checks if an error is of type AppError and extracts it
func As(err error, target **AppError) bool {
	if err == nil {
		return false
	}
	if appErr, ok := err.(*AppError); ok {
		*target = appErr
		return true
	}
	// Handle wrapped errors
	if unwrapped := Unwrap(err); unwrapped != err {
		return As(unwrapped, target)
	}
	return false
}

// Unwrap unwraps an error to get the underlying error
func Unwrap(err error) error {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Unwrap()
	}
	if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
		return unwrapper.Unwrap()
	}
	return err
}

// Is checks if an error is of a specific type
func Is(err error, errorType ErrorType) bool {
	var appErr *AppError
	if As(err, &appErr) {
		return appErr.Type == errorType
	}
	return false
}

// GetErrorType returns the error type if it's an AppError
func GetErrorType(err error) (ErrorType, bool) {
	var appErr *AppError
	if As(err, &appErr) {
		return appErr.Type, true
	}
	return ErrorTypeUnknown, false
}

// UserFriendlyMessage generates a user-friendly error message
func UserFriendlyMessage(err error) string {
	var appErr *AppError
	if As(err, &appErr) {
		// Create user-friendly messages based on error type
		switch appErr.Type {
		case ErrorTypeNetwork:
			return "Network error: Please check your internet connection and try again."
		case ErrorTypeValidation:
			return "Invalid input: Please check your request and try again."
		case ErrorTypeIO:
			return "File operation failed: Please check file permissions and paths."
		case ErrorTypeParse:
			return "Data processing error: There was an issue processing the response."
		case ErrorTypePermission:
			return "Permission denied: You don't have permission to perform this action."
		case ErrorTypeTimeout:
			return "Request timed out: The operation took too long to complete."
		default:
			return "An unexpected error occurred. Please try again."
		}
	}
	return fmt.Sprintf("An error occurred: %v", err)
}

// ExtractContext extracts context from an AppError
func ExtractContext(err error) map[string]interface{} {
	var appErr *AppError
	if As(err, &appErr) {
		return appErr.Context
	}
	return nil
}

// ErrorChain returns the full error chain as a string
func ErrorChain(err error) string {
	var parts []string
	current := err
	for current != nil {
		parts = append(parts, current.Error())
		if unwrapper, ok := current.(interface{ Unwrap() error }); ok {
			current = unwrapper.Unwrap()
		} else {
			break
		}
	}
	return strings.Join(parts, " \u2192 ")
}