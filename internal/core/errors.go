package core

import "fmt"

type ErrorCode string

const (
	ErrorCodeUnknown      ErrorCode = "unknown"
	ErrorCodeCancelled    ErrorCode = "cancelled"
	ErrorCodeConflict     ErrorCode = "conflict"
	ErrorCodeInternal     ErrorCode = "internal"
	ErrorCodeNotFound     ErrorCode = "not_found"
	ErrorCodeTimeout      ErrorCode = "timeout"
	ErrorCodeUnsupported  ErrorCode = "unsupported"
	ErrorCodeUnauthorized ErrorCode = "unauthorized"
	ErrorCodeValidation   ErrorCode = "validation"
)

type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
	Details map[string]string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" && e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

func WrapError(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func (e *Error) WithDetail(key, value string) *Error {
	if e == nil {
		return nil
	}
	if e.Details == nil {
		e.Details = make(map[string]string, 1)
	}
	e.Details[key] = value
	return e
}

func (c ErrorCode) String() string {
	return string(c)
}

func (c ErrorCode) GoString() string {
	return fmt.Sprintf("ErrorCode(%q)", string(c))
}
