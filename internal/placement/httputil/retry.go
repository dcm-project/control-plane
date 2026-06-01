// Package httputil provides HTTP utility functions including retry logic.
package httputil

import (
	"time"

	"github.com/cenkalti/backoff/v5"
)

// DefaultRetryOpts returns the standard retry options used across HTTP clients.
func DefaultRetryOpts() []backoff.RetryOption {
	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.InitialInterval = 1 * time.Second
	expBackoff.Multiplier = 2
	expBackoff.MaxInterval = 4 * time.Second

	return []backoff.RetryOption{
		backoff.WithBackOff(expBackoff),
		backoff.WithMaxTries(4),
	}
}

// IsPermanentHTTPError returns true if the HTTP status code represents a
// permanent (non-retriable) error. Transient errors like 408 (Request Timeout)
// and 429 (Too Many Requests) are retriable; all other 4xx are permanent.
func IsPermanentHTTPError(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != 408 && statusCode != 429
}
