package store

import (
	"encoding/base64"
	"errors"
	"testing"
)

func Test_encodePageToken_openAPIExample(t *testing.T) {
	got, err := encodePageToken(100)
	if err != nil {
		t.Fatalf("encodePageToken: %v", err)
	}
	want := base64.StdEncoding.EncodeToString([]byte(`{"offset":100}`))
	if got != want {
		t.Fatalf("encodePageToken: got %q want %q", got, want)
	}
}

func Test_decodePageToken_openAPIExample(t *testing.T) {
	token := base64.StdEncoding.EncodeToString([]byte(`{"offset":100}`))
	offset, err := decodePageToken(token)
	if err != nil {
		t.Fatalf("decodePageToken: %v", err)
	}
	if offset != 100 {
		t.Fatalf("offset: got %d", offset)
	}
}

func Test_decodePageToken_invalid(t *testing.T) {
	_, err := decodePageToken("not-valid-base64!!!")
	if !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("expected ErrInvalidPageToken, got %v", err)
	}

	legacy := base64.StdEncoding.EncodeToString([]byte("100"))
	_, err = decodePageToken(legacy)
	if !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("legacy token: expected ErrInvalidPageToken, got %v", err)
	}
}
