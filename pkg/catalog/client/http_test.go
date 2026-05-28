package client

import (
	"testing"
	"time"
)

func TestAPIBaseURL(t *testing.T) {
	got, err := APIBaseURL("http://localhost:8080/")
	if err != nil {
		t.Fatalf("APIBaseURL: %v", err)
	}
	if got != "http://localhost:8080/api/v1alpha1" {
		t.Fatalf("APIBaseURL: got %q", got)
	}
}

func TestDefaultHTTPClient_timeout(t *testing.T) {
	c := DefaultHTTPClient(5 * time.Second)
	if c.Timeout != 5*time.Second {
		t.Fatalf("Timeout: got %v", c.Timeout)
	}
}
