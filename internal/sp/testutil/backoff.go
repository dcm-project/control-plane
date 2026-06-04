// Package testutil provides helpers shared by tests in this module.
package testutil

import (
	"time"

	"github.com/cenkalti/backoff/v5"
)

// FastServiceTypeInstanceRetry returns backoff.RetryOption values for tests that
// construct the service-type-instance store (Create/HardDelete retry paths).
// Delays stay in the millisecond range so retry exhaustion does not stall suites.
func FastServiceTypeInstanceRetry() []backoff.RetryOption {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = time.Millisecond
	b.MaxInterval = time.Millisecond
	b.Multiplier = 1.0
	return []backoff.RetryOption{
		backoff.WithBackOff(b),
		backoff.WithMaxTries(4),
	}
}
