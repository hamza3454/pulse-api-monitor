package domain

import (
	"errors"
	"regexp"
	"strings"
)

// ErrNotFound is returned by stores when a record does not exist. Callers
// check for it with errors.Is.
var ErrNotFound = errors.New("not found")

// ValidationError lists every problem found while validating a value, so the
// API can report them all at once instead of one per request.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return strings.Join(e.Problems, "; ")
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsValidID reports whether s is a well-formed UUID. IDs come from URLs, so
// they are checked before being sent to the database.
func IsValidID(s string) bool {
	return uuidPattern.MatchString(s)
}
