package client

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultHTTPTimeout = 10 * time.Second

// DefaultHTTPClient returns an HTTP client with a request timeout.
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	return &http.Client{Timeout: timeout}
}

// APIBaseURL appends the catalog API version path to a service base URL.
func APIBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base URL is required")
	}
	return url.JoinPath(strings.TrimRight(baseURL, "/"), "api", "v1alpha1")
}
