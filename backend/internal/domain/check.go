package domain

import (
	"crypto/rand"
	"fmt"
	"time"
)

// CheckResult is the outcome of one check of one monitor.
//
// StatusCode, LatencyMS and Error are pointers because "no value" is
// meaningful: a timed-out request has no status code, and a successful check
// has no error. They map to NULL in the database and null in the API's JSON.
type CheckResult struct {
	ID         int64 // database row ID; zero until stored
	MonitorID  string
	JobID      string // unique per check; makes storing a result idempotent
	CheckedAt  time.Time
	StatusCode *int
	LatencyMS  *int
	Success    bool
	Error      *string
}

// NewID returns a random (version 4) UUID. It is used for check job IDs. It is
// written out here instead of importing a UUID library because that is all
// Pulse needs from one.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the OS has no randomness; nothing sensible
		// can continue.
		panic(fmt.Sprintf("domain: read random bytes: %v", err))
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
