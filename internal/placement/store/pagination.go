package store

import (
	"encoding/base64"
	"strconv"
)

// decodePageToken decodes a base64-encoded page token to get the offset.
// Returns 0 if the token is nil, empty, or invalid.
func decodePageToken(pageToken *string) int {
	if pageToken == nil || *pageToken == "" {
		return 0
	}

	decoded, err := base64.StdEncoding.DecodeString(*pageToken)
	if err != nil {
		return 0
	}

	offset, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0
	}

	return offset
}

// encodePageToken encodes an offset as a base64 page token.
func encodePageToken(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

// generateNextPageToken generates a next page token if there are more results.
// Returns nil if there are no more results (results <= pageSize).
func generateNextPageToken(resultCount, pageSize, currentOffset int) *string {
	if resultCount <= pageSize {
		return nil
	}

	nextOffset := currentOffset + pageSize
	token := encodePageToken(nextOffset)
	return &token
}
