package service

import "time"

// PtrTime returns a pointer to the given time, or nil if it's zero.
func PtrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
