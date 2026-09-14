package problem

import (
	"net/http"
	"testing"
)

func TestAuthErrorFields(t *testing.T) {
	p := AuthErrorFields(http.StatusUnauthorized, "missing token")
	if p.Type != TypeUnauthenticated || p.Status != 401 || p.Detail != "missing token" {
		t.Fatalf("unexpected auth fields: %+v", p)
	}

	p = AuthErrorFields(http.StatusForbidden, "suspended")
	if p.Type != TypePermissionDenied {
		t.Fatalf("forbidden type: %q", p.Type)
	}
}

func TestValidationErrorFields(t *testing.T) {
	p := ValidationErrorFields(http.StatusBadRequest, "bad field")
	if p.Type != TypeInvalidArgument {
		t.Fatalf("type: %q", p.Type)
	}
}
