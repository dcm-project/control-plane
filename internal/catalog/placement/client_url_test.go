package placement

import "testing"

func TestAPIBaseURL(t *testing.T) {
	got, err := apiBaseURL("http://placement:8081/")
	if err != nil {
		t.Fatalf("apiBaseURL: %v", err)
	}
	if got != "http://placement:8081/api/v1alpha1" {
		t.Fatalf("apiBaseURL: got %q", got)
	}
}
