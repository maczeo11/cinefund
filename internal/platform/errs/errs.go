// Package errs gives every error a Kind, and maps Kind to an HTTP status in
// exactly one place.
//
// The rule: handlers never call c.JSON(500, ...). They return an error and the
// error middleware decides the status. That way "what status does a duplicate
// pledge produce" has one answer, in one file, instead of forty.
package errs

import (
	"errors"
	"fmt"
	"net/http"
)

type Kind string

const (
	KindInvalid      Kind = "INVALID"       // 400 - malformed or failed validation
	KindUnauthorized Kind = "UNAUTHORIZED"  // 401 - no or bad credentials
	KindForbidden    Kind = "FORBIDDEN"     // 403 - authenticated, not allowed
	KindNotFound     Kind = "NOT_FOUND"     // 404
	KindConflict     Kind = "CONFLICT"      // 409 - state machine or uniqueness
	KindUnprocessable Kind = "UNPROCESSABLE" // 422 - semantically wrong
	KindRateLimited  Kind = "RATE_LIMITED"  // 429
	KindInternal     Kind = "INTERNAL"      // 500
	KindUnavailable  Kind = "UNAVAILABLE"   // 503 - a dependency is down
)

// Error carries a machine-readable Code for clients, a human Message, optional
// structured Details (e.g. per-field validation failures), and a wrapped cause
// that is logged but never serialised to the client.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Details any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// WithDetails attaches structured detail (validation failures, for instance).
func (e *Error) WithDetails(d any) *Error { e.Details = d; return e }

// WithCause attaches an underlying error for the logs. Never shown to clients.
func (e *Error) WithCause(err error) *Error { e.cause = err; return e }

func newf(k Kind, code, format string, args ...any) *Error {
	return &Error{Kind: k, Code: code, Message: fmt.Sprintf(format, args...)}
}

func Invalid(code, format string, a ...any) *Error {
	return newf(KindInvalid, code, format, a...)
}
func Unauthorized(code, format string, a ...any) *Error {
	return newf(KindUnauthorized, code, format, a...)
}
func Forbidden(code, format string, a ...any) *Error {
	return newf(KindForbidden, code, format, a...)
}
func NotFound(code, format string, a ...any) *Error {
	return newf(KindNotFound, code, format, a...)
}
func Conflict(code, format string, a ...any) *Error {
	return newf(KindConflict, code, format, a...)
}
func Unprocessable(code, format string, a ...any) *Error {
	return newf(KindUnprocessable, code, format, a...)
}
func RateLimited(code, format string, a ...any) *Error {
	return newf(KindRateLimited, code, format, a...)
}
func Internal(code, format string, a ...any) *Error {
	return newf(KindInternal, code, format, a...)
}
func Unavailable(code, format string, a ...any) *Error {
	return newf(KindUnavailable, code, format, a...)
}

// HTTPStatus maps any error to a status code. An error that is not an *Error is
// a bug that escaped, so it maps to 500 - deliberately, rather than guessing.
func HTTPStatus(err error) int {
	var e *Error
	if !errors.As(err, &e) {
		return http.StatusInternalServerError
	}
	switch e.Kind {
	case KindInvalid:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindUnprocessable:
		return http.StatusUnprocessableEntity
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Code returns the client-facing code, or a generic one for unwrapped errors.
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "INTERNAL_ERROR"
}
