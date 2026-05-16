package whatttft

import "time"

// Clock provides the current time with any monotonic component preserved for duration calculations.
type Clock interface {
	// Now returns the current time; implementations should preserve Go's monotonic clock reading when possible.
	Now() time.Time
}

// RealClock reads the process wall and monotonic clock using time.Now.
type RealClock struct{}

// Now returns time.Now with Go's monotonic clock component intact.
func (RealClock) Now() time.Time {
	return time.Now()
}
